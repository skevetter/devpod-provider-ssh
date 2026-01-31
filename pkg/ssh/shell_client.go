package ssh

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/kballard/go-shellquote"
	"github.com/skevetter/devpod-provider-ssh/pkg/options"
	"github.com/skevetter/log"
)

// ShellSSHClient implements SSHClient using command-line ssh/scp.
type ShellSSHClient struct {
	config *options.Options
	log    log.Logger
}

// NewShellSSHClient creates a new shell-based SSH client.
func NewShellSSHClient(config *options.Options, logger log.Logger) *ShellSSHClient {
	return &ShellSSHClient{
		config: config,
		log:    logger,
	}
}

// Connect is a no-op for shell client (connection happens per command).
func (c *ShellSSHClient) Connect() error {
	return nil
}

// Execute runs a command via ssh binary.
func (c *ShellSSHClient) Execute(command string, output io.Writer) error {
	commandToRun, err := c.getSSHCommand()
	if err != nil {
		return err
	}

	commandToRun = append(commandToRun, command)

	var stderrBuf bytes.Buffer

	// #nosec G204 -- commandToRun is built from validated config
	cmd := exec.Command("ssh", commandToRun...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = output
	cmd.Stderr = io.Writer(&stderrBuf)

	err = cmd.Run()
	if err != nil {
		c.log.Error(stderrBuf.String())
		return err
	}

	// A non-POSIX shell has been detected: falling back to copy and execute scripts
	if strings.Contains(stderrBuf.String(), "fish: Unsupported") {
		c.log.Warn("non-posix shell detected, using script upload")
		return c.copyAndExecute(command, output)
	}

	return nil
}

// Upload transfers a file using scp.
func (c *ShellSSHClient) Upload(localPath, remotePath string) error {
	commandToRun, err := c.getSCPCommand(localPath, remotePath)
	if err != nil {
		return err
	}

	// #nosec G204 -- commandToRun is built from validated config
	return exec.Command("scp", commandToRun...).Run()
}

// Close is a no-op for shell client.
func (c *ShellSSHClient) Close() error {
	return nil
}

// getSSHCommand builds the ssh command arguments.
func (c *ShellSSHClient) getSSHCommand() ([]string, error) {
	result := []string{"-oStrictHostKeyChecking=no", "-oBatchMode=yes"}

	if c.config.Port != "22" {
		result = append(result, []string{"-p", c.config.Port}...)
	}

	if c.config.ExtraFlags != "" {
		flags, err := shellquote.Split(c.config.ExtraFlags)
		if err != nil {
			return nil, fmt.Errorf("parse extra flags: %w", err)
		}
		result = append(result, flags...)
	}

	result = append(result, c.config.Host)
	return result, nil
}

// getSCPCommand builds the scp command arguments.
func (c *ShellSSHClient) getSCPCommand(sourcefile, destfile string) ([]string, error) {
	result := []string{"-oStrictHostKeyChecking=no", "-oBatchMode=yes"}

	if c.config.Port != "22" {
		result = append(result, []string{"-P", c.config.Port}...)
	}

	if c.config.ExtraFlags != "" {
		flags, err := shellquote.Split(c.config.ExtraFlags)
		if err != nil {
			return nil, fmt.Errorf("parse extra flags: %w", err)
		}
		result = append(result, flags...)
	}

	result = append(result, sourcefile)
	result = append(result, c.config.Host+":"+destfile)
	return result, nil
}

// copyAndExecute handles non-POSIX shells by uploading script and executing.
func (c *ShellSSHClient) copyAndExecute(command string, output io.Writer) error {
	script, err := c.copyCommandToRemote(command)
	if err != nil {
		return err
	}

	commandToRun, err := c.getSSHCommand()
	if err != nil {
		return err
	}

	commandToRun = append(commandToRun, []string{
		"/bin/sh", script, ";", "rm", "-f", script,
	}...)

	// #nosec G204 -- commandToRun is built from validated config
	cmd := exec.Command("ssh", commandToRun...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = output
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// copyCommandToRemote creates a temp script and uploads it.
func (c *ShellSSHClient) copyCommandToRemote(command string) (string, error) {
	script, err := os.CreateTemp("", "devpod-command-*")
	if err != nil {
		return "", err
	}
	defer func() {
		_ = script.Close()
		_ = os.Remove(script.Name())
	}()

	_, err = script.WriteString(command)
	if err != nil {
		return "", err
	}

	destfile := "/tmp/" + filepath.Base(script.Name())
	err = c.Upload(script.Name(), destfile)
	if err != nil {
		return "", err
	}

	return destfile, nil
}
