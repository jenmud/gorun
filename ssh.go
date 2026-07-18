package main

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/crypto/ssh"
)

// SSH represents SSH creds
type SSH struct {
	User         string `toml:"user"`
	Password     string `toml:"password"`
	Port         int    `toml:"port"`
	Identifyfile string `toml:"identityfile"`
}

// NewSSHClient creates a new SSH client connected to the provided server.
func NewSSHClient(ctx context.Context, server Server) (*ssh.Client, error) {
	var hostkey ssh.PublicKey

	config := &ssh.ClientConfig{
		Config: ssh.Config{},
		User:   server.SSH.User,
		Auth: []ssh.AuthMethod{
			ssh.Password(server.SSH.Password),
		},
		HostKeyCallback: ssh.FixedHostKey(hostkey),
		Timeout:         30 * time.Second,
	}

	client, err := ssh.Dial("tcp", server.Hostname, config)
	if err != nil {
		return nil, fmt.Errorf("error creating SSH client: %w", err)
	}

	return client, nil
}
