package gorun

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/ssh"
)

// SSH represents SSH creds
type SSH struct {
	User         string `toml:"username"`
	Password     string `toml:"password"`
	Port         int    `toml:"port"`
	Identityfile string `toml:"identityfile"`
}

// NewSSHClient creates a new SSH client connected to the provided server.
func NewSSHClient(ctx context.Context, server Server) (*ssh.Client, error) {
	port := 22

	if server.SSH.Port != 0 {
		port = server.SSH.Port
	}

	address := fmt.Sprintf("%s:%d", server.Hostname, port)
	auth := []ssh.AuthMethod{}

	// Create the Signer for this private key.
	if server.SSH.Identityfile != "" {
		key, err := os.ReadFile(server.SSH.Identityfile)
		if err != nil {
			err = fmt.Errorf("error reading SSH IdentityFile %q: %w", server.SSH.Identityfile, err)
			return nil, err
		}

		signer, err := ssh.ParsePrivateKey(key)
		if err != nil {
			err = fmt.Errorf("error parsing SSH IdentityFile %q: %w", server.SSH.Identityfile, err)
			return nil, err
		}

		auth = append(auth, ssh.PublicKeys(signer))
	}

	// if we have a SSH password, add it to the list of auths
	if server.SSH.Password != "" {
		auth = append(auth, ssh.Password(server.SSH.Password))
	}

	config := &ssh.ClientConfig{
		Config:          ssh.Config{},
		User:            server.SSH.User,
		Auth:            auth,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         30 * time.Second,
	}

	client, err := ssh.Dial("tcp", address, config)
	if err != nil {
		return nil, fmt.Errorf("error creating SSH client: %w", err)
	}

	return client, nil
}

// scpCopy copies a local file to remote using scp via SSH session.
func scpCopy(client *ssh.Client, localPath, remotePath string, mode os.FileMode) error {
	if client == nil {
		// try local copy
		info, err := os.Stat(localPath)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(localPath)
		if err != nil {
			return err
		}
		if mode == 0 {
			mode = info.Mode()
		}
		return os.WriteFile(remotePath, data, mode)
	}

	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	info, err := os.Stat(localPath)
	if err != nil {
		return err
	}
	if mode == 0 {
		mode = info.Mode().Perm()
	}

	file, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer file.Close()

	// Use scp protocol
	go func() {
		w, _ := session.StdinPipe()
		defer w.Close()
		fmt.Fprintf(w, "C%04o %d %s\n", mode, info.Size(), filepath.Base(remotePath))
		io.CopyN(w, file, info.Size())
		fmt.Fprint(w, "\x00")
	}()

	if err := session.Run("/usr/bin/scp -t " + filepath.Dir(remotePath)); err != nil {
		// try without /usr/bin/scp if dir specified differently
		if err := session.Run("scp -t " + filepath.Dir(remotePath)); err != nil {
			return err
		}
	}
	return nil
}

// CopyFiles copies files for a task to a server
func CopyFiles(client *ssh.Client, serverName string, task *Task) error {
	copies := task.getCopies()
	for _, c := range copies {
		dst := c.Dst
		if dst == "" && c.Dir != "" {
			dst = filepath.Join(c.Dir, filepath.Base(c.Src))
		}
		if err := scpCopy(client, c.Src, dst, 0); err != nil {
			return err
		}
	}
	return nil
}

