package gorun

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"golang.org/x/crypto/ssh"
)

// modeOf returns the permission bits a copy sets, and whether it sets any. mode, or its alias
// perm, is an octal permission such as "0644". Only the nine permission bits are kept: setuid,
// setgid and sticky bits are not set by a copy.
func (c Copy) modeOf() (os.FileMode, bool, error) {
	s := strings.TrimSpace(cmp.Or(c.Mode, c.Perm))
	if s == "" {
		return 0, false, nil
	}
	n, err := strconv.ParseUint(s, 8, 32)
	if err != nil || n > 0o7777 {
		return 0, false, fmt.Errorf("mode %q is not an octal permission such as 0644", s)
	}
	return os.FileMode(n) & os.ModePerm, true, nil
}

// ownerSpec is what chown takes for a copy's owner and group: "user", ":group" or
// "user:group". It is empty when the copy sets neither.
func (c Copy) ownerSpec() string {
	switch {
	case c.Owner != "" && c.Group != "":
		return c.Owner + ":" + c.Group
	case c.Owner != "":
		return c.Owner
	case c.Group != "":
		return ":" + c.Group
	}
	return ""
}

// setsPerms reports whether the copy sets a mode, an owner or a group.
func (c Copy) setsPerms() bool {
	_, hasMode, _ := c.modeOf()
	return hasMode || c.ownerSpec() != ""
}

// applyPerms sets the mode and owner a copy asks for on dst, after the copy has written it.
// A file gets the mode itself. A folder gets the mode on every file inside it, and the owner on
// the whole tree (chown -R). The commands run on the server over ssh, or on this machine when
// client is nil, under sudo -n when the task has sudo set (chown needs root to hand a file to
// another user). They run as the login user otherwise, as the copy does.
func applyPerms(ctx context.Context, client *ssh.Client, c Copy, dst string, dir, sudo bool) error {
	command, err := permCommand(c, dst, dir, sudo)
	if err != nil || command == "" {
		return err
	}
	if client != nil {
		return runRemote(ctx, client, command)
	}
	out, err := exec.CommandContext(ctx, "sh", "-c", command).CombinedOutput()
	if err != nil {
		return fmt.Errorf("setting the copy's mode or owner on %s: %w: %s", dst, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// permCommand is the shell command that sets a copy's mode and owner on dst, or "" when the copy
// sets neither. See applyPerms.
func permCommand(c Copy, dst string, dir, sudo bool) (string, error) {
	mode, hasMode, err := c.modeOf()
	if err != nil {
		return "", err
	}
	owner := c.ownerSpec()
	if !hasMode && owner == "" {
		return "", nil
	}
	var steps []string
	if hasMode {
		if dir {
			steps = append(steps, fmt.Sprintf("find %s -type f -exec chmod %04o {} +", shellQuote(dst), mode))
		} else {
			steps = append(steps, fmt.Sprintf("chmod %04o %s", mode, shellQuote(dst)))
		}
	}
	if owner != "" {
		rec := ""
		if dir {
			rec = "-R "
		}
		steps = append(steps, "chown "+rec+shellQuote(owner)+" "+shellQuote(dst))
	}
	command := strings.Join(steps, " && ")
	if sudo {
		command = sudoWrap(command)
	}
	return command, nil
}

// runRemote runs one command on the server and reports its error, with its stderr.
func runRemote(ctx context.Context, client *ssh.Client, command string) error {
	session, closeSession, err := newSession(ctx, client)
	if err != nil {
		return err
	}
	defer closeSession()
	var stderr bytes.Buffer
	session.Stderr = &stderr
	if err := session.Run(command); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("%w: %s", err, msg)
		}
		return err
	}
	return nil
}

// permLine is what `ls -ld` says about a copy's target: its nine permission characters, and
// its owner and group names.
type permLine struct {
	perm, user, group string
}

// lsScript prints a "p <n> <perm> <user> <group>" line for the target of copy n, as ls -ld
// shows it, or nothing when the target isn't there. perm is the nine permission characters, without
// the file type. It works wherever ls -l does.
func lsScript(n int, dst string) string {
	return fmt.Sprintf("ls -ld -- %s 2>/dev/null | awk '{print \"p %d \" substr($1,2,9) \" \" $3 \" \" $4}'", shellQuote(dst), n)
}

// lsLocal is lsScript's answer for a path on this machine.
func lsLocal(ctx context.Context, path string) (permLine, bool) {
	out, err := exec.CommandContext(ctx, "ls", "-ld", "--", path).Output()
	if err != nil {
		return permLine{}, false
	}
	f := strings.Fields(string(out))
	if len(f) < 4 || len(f[0]) < 10 {
		return permLine{}, false
	}
	return permLine{perm: f[0][1:10], user: f[2], group: f[3]}, true
}

// parsePerms reads the "p" lines of a check's output into the copy each one is about.
func parsePerms(output string) map[int]permLine {
	out := map[int]permLine{}
	for _, line := range strings.Split(output, "\n") {
		f := strings.Fields(line)
		if len(f) != 5 || f[0] != "p" {
			continue
		}
		n, err := strconv.Atoi(f[1])
		if err != nil {
			continue
		}
		out[n] = permLine{perm: f[2], user: f[3], group: f[4]}
	}
	return out
}

// permOctal turns ls's nine permission characters, such as rwxr-x---, into their permission bits.
// setuid, setgid and sticky show as s, S, t or T, and count as execute when they are lower case.
func permOctal(s string) (int, bool) {
	if len(s) != 9 {
		return 0, false
	}
	n := 0
	for i, ch := range s {
		bit := 1 << (8 - i) // position 0 is user read, 8 is other execute
		switch i % 3 {
		case 0:
			if ch == 'r' {
				n |= bit
			} else if ch != '-' {
				return 0, false
			}
		case 1:
			if ch == 'w' {
				n |= bit
			} else if ch != '-' {
				return 0, false
			}
		case 2:
			switch ch {
			case 'x', 's', 't':
				n |= bit
			case '-', 'S', 'T':
			default:
				return 0, false
			}
		}
	}
	return n, true
}

// permMismatch says how a copy's target differs from what the copy would set, or returns "" when
// it matches. A target that isn't there is a mismatch.
func permMismatch(c Copy, got permLine, found bool) string {
	if !found {
		return "is missing"
	}
	var diffs []string
	mode, hasMode, _ := c.modeOf()
	if hasMode {
		have, ok := permOctal(got.perm)
		if !ok || have != int(mode) {
			diffs = append(diffs, fmt.Sprintf("mode %04o wanted, %s found", mode, got.perm))
		}
	}
	if c.Owner != "" && got.user != c.Owner {
		diffs = append(diffs, fmt.Sprintf("owner %s wanted, %s found", c.Owner, got.user))
	}
	if c.Group != "" && got.group != c.Group {
		diffs = append(diffs, fmt.Sprintf("group %s wanted, %s found", c.Group, got.group))
	}
	return strings.Join(diffs, "; ")
}
