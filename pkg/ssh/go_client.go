package ssh

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"github.com/skevetter/devpod-provider-ssh/pkg/options"
	"github.com/skevetter/log"
	"golang.org/x/crypto/ssh"
)

// GoSSHClient implements SSHClient using pure Go SSH.
type GoSSHClient struct {
	config     *options.Options
	log        log.Logger
	sshClient  *ssh.Client
	sshConfig  *ssh.ClientConfig
	remoteAddr string

	// Connection lifecycle
	lastUsed    time.Time
	connectedAt time.Time
	maxIdleTime time.Duration
	maxLifetime time.Duration
	mu          sync.RWMutex
}

// NewGoSSHClient creates a new Go-based SSH client.
func NewGoSSHClient(config *options.Options, logger log.Logger) *GoSSHClient {
	return &GoSSHClient{
		config:      config,
		log:         logger,
		maxIdleTime: 5 * time.Minute,
		maxLifetime: 1 * time.Hour,
	}
}

// Connect establishes the SSH connection.
func (c *GoSSHClient) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	sshConfig, err := ParseSSHConfig(c.config.Host, "")
	if err != nil {
		return fmt.Errorf("parse ssh config: %w", err)
	}

	if c.config.Port != "" && c.config.Port != "22" {
		sshConfig.Port = c.config.Port
	}

	authMethods, err := c.loadAuthMethods(sshConfig.IdentityFiles)
	if err != nil {
		return err
	}

	c.sshConfig = &ssh.ClientConfig{
		User: sshConfig.User,
		Auth: authMethods,
		// #nosec G106 -- InsecureIgnoreHostKey is acceptable for DevPod use case
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         30 * time.Second,
	}

	c.remoteAddr = net.JoinHostPort(sshConfig.Hostname, sshConfig.Port)
	client, err := ssh.Dial("tcp", c.remoteAddr, c.sshConfig)
	if err != nil {
		return fmt.Errorf("ssh dial: %w", err)
	}

	c.sshClient = client
	c.connectedAt = time.Now()
	c.lastUsed = time.Now()
	c.log.Debugf("connected to %s via pure go ssh", c.remoteAddr)
	return nil
}

// Execute runs a command on the remote host.
func (c *GoSSHClient) Execute(command string, output io.Writer) error {
	client, err := c.ensureConnected()
	if err != nil {
		return err
	}

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	defer func() { _ = session.Close() }()

	var stderrBuf strings.Builder
	session.Stdout = output
	session.Stderr = io.MultiWriter(output, &stderrBuf)
	session.Stdin = os.Stdin

	err = session.Run(command)

	// Check for non-POSIX shell
	if err != nil && strings.Contains(stderrBuf.String(), "fish: Unsupported") {
		c.log.Warn("non-posix shell detected, using script upload")
		return c.executeViaScript(command, output)
	}

	return err
}

// Upload transfers a file to the remote host using SFTP.
func (c *GoSSHClient) Upload(localPath, remotePath string) error {
	client, err := c.ensureConnected()
	if err != nil {
		return err
	}

	return c.uploadFile(client, localPath, remotePath)
}

// Close terminates the SSH connection.
func (c *GoSSHClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.sshClient != nil {
		err := c.sshClient.Close()
		c.sshClient = nil
		return err
	}
	return nil
}

// isStale checks if the connection should be refreshed.
func (c *GoSSHClient) isStale() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.sshClient == nil {
		return true
	}

	// Check idle timeout
	if time.Since(c.lastUsed) > c.maxIdleTime {
		c.log.Debugf("connection idle for %v, exceeds max %v", time.Since(c.lastUsed), c.maxIdleTime)
		return true
	}

	// Check max lifetime
	if time.Since(c.connectedAt) > c.maxLifetime {
		c.log.Debugf("connection age %v, exceeds max %v", time.Since(c.connectedAt), c.maxLifetime)
		return true
	}

	return false
}

// reconnect closes the current connection and establishes a new one.
func (c *GoSSHClient) reconnect() error {
	c.mu.Lock()
	if c.sshClient != nil {
		_ = c.sshClient.Close()
		c.sshClient = nil
	}
	c.mu.Unlock()

	return c.Connect()
}

// ensureConnected checks staleness, reconnects if needed, and returns the client.
func (c *GoSSHClient) ensureConnected() (*ssh.Client, error) {
	if c.isStale() {
		c.log.Debug("connection stale, reconnecting")
		if err := c.reconnect(); err != nil {
			return nil, fmt.Errorf("reconnect failed: %w", err)
		}
	}

	c.mu.Lock()
	c.lastUsed = time.Now()
	client := c.sshClient
	c.mu.Unlock()

	if client == nil {
		return nil, fmt.Errorf("not connected")
	}

	return client, nil
}

func (c *GoSSHClient) uploadFile(client *ssh.Client, localPath, remotePath string) error {
	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		return fmt.Errorf("create sftp client: %w", err)
	}
	defer func() { _ = sftpClient.Close() }()

	// #nosec G304 -- localPath is controlled by provider
	localFile, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("open local file: %w", err)
	}
	defer func() { _ = localFile.Close() }()

	remoteFile, err := sftpClient.Create(remotePath)
	if err != nil {
		return fmt.Errorf("create remote file: %w", err)
	}
	defer func() { _ = remoteFile.Close() }()

	_, err = io.Copy(remoteFile, localFile)
	if err != nil {
		return fmt.Errorf("copy file: %w", err)
	}

	return nil
}

// executeViaScript uploads command as script and executes it.
func (c *GoSSHClient) executeViaScript(command string, output io.Writer) error {
	client, err := c.ensureConnected()
	if err != nil {
		return err
	}

	tmpFile, err := os.CreateTemp("", "devpod-command-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer func() {
		_ = tmpFile.Close()
		_ = os.Remove(tmpFile.Name())
	}()

	if _, err := tmpFile.WriteString(command); err != nil {
		return fmt.Errorf("write script: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}

	remotePath := "/tmp/" + filepath.Base(tmpFile.Name())
	if err := c.uploadFile(client, tmpFile.Name(), remotePath); err != nil {
		return fmt.Errorf("upload script: %w", err)
	}

	cleanupCmd := fmt.Sprintf("/bin/sh %s; rm -f %s", remotePath, remotePath)
	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	defer func() { _ = session.Close() }()

	session.Stdout = output
	session.Stderr = output
	session.Stdin = os.Stdin

	return session.Run(cleanupCmd)
}

// loadAuthMethods loads SSH authentication methods from identity files.
func (c *GoSSHClient) loadAuthMethods(identityFiles []string) ([]ssh.AuthMethod, error) {
	var authMethods []ssh.AuthMethod

	for _, keyPath := range identityFiles {
		if _, err := os.Stat(keyPath); os.IsNotExist(err) {
			continue
		}

		// #nosec G304 -- keyPath is from SSH config
		key, err := os.ReadFile(keyPath)
		if err != nil {
			c.log.Debugf("failed to read key %s: %v", keyPath, err)
			continue
		}

		signer, err := ssh.ParsePrivateKey(key)
		if err != nil {
			c.log.Debugf("failed to parse key %s: %v", keyPath, err)
			continue
		}

		authMethods = append(authMethods, ssh.PublicKeys(signer))
		c.log.Debugf("loaded ssh key: %s", keyPath)
	}

	if len(authMethods) == 0 {
		return nil, &KeyFormatError{Format: "no valid keys found"}
	}

	return authMethods, nil
}
