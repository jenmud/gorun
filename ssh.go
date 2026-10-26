package gorun

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"text/template"
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

// scpCopy copies data to remote using scp via SSH session.
func scpCopy(client *ssh.Client, data []byte, remotePath string, mode os.FileMode) error {
	if client == nil {
		if mode == 0 {
			mode = 0644
		}
		return os.WriteFile(remotePath, data, mode)
	}

	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	if mode == 0 {
		mode = 0644
	}

	go func() {
		w, _ := session.StdinPipe()
		defer w.Close()
		fmt.Fprintf(w, "C%04o %d %s\n", mode.Perm(), len(data), filepath.Base(remotePath))
		w.Write(data)
		fmt.Fprint(w, "\x00")
	}()

	if err := session.Run("/usr/bin/scp -t " + filepath.Dir(remotePath)); err != nil {
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
		cc := c
		if cc.SrcFile != "" && cc.Src == "" {
			cc.Src = cc.SrcFile
		}
		if cc.Template != "" && cc.SrcTpl == "" {
			cc.SrcTpl = cc.Template
		}
		if cc.SrcTmpl != "" && cc.SrcTpl == "" {
			cc.SrcTpl = cc.SrcTmpl
		}
		if cc.DstPath != "" && cc.Dst == "" {
			cc.Dst = cc.DstPath
		}
		if cc.Dest != "" && cc.Dst == "" {
			cc.Dst = cc.Dest
		}
		dst := cc.Dst
		srcPath := cc.Src
		if dst == "" && cc.Dir != "" && srcPath != "" {
			dst = filepath.Join(cc.Dir, filepath.Base(srcPath))
		}

		var data []byte
		var err error
		if cc.Content != "" {
			data = []byte(cc.Content)
		} else if cc.SrcTpl != "" {
			tmplData, err := os.ReadFile(cc.SrcTpl)
			if err != nil {
				return err
			}
			tmpl, err := template.New(filepath.Base(cc.SrcTpl)).Parse(string(tmplData))
			if err != nil {
				return err
			}
			var buf bytes.Buffer
			var srv any
			// try to find server
			srv = Server{Name: serverName}
			if err := tmpl.Execute(&buf, srv); err != nil {
				return err
			}
			data = buf.Bytes()
		} else if srcPath != "" {
			data, err = os.ReadFile(srcPath)
			if err != nil {
				return err
			}
		} else {
			return fmt.Errorf("no source for copy")
		}

		if err := scpCopy(client, data, dst, 0); err != nil {
			return err
		}
	}
	return nil
}

