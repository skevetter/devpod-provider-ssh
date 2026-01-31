package ssh

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/skevetter/devpod-provider-ssh/pkg/options"
	"github.com/skevetter/log"
)

type SSHProvider struct {
	Config           *options.Options
	Log              log.Logger
	WorkingDirectory string
	client           SSHClient
}

func NewProvider(logs log.Logger) (*SSHProvider, error) {
	config, err := options.FromEnv()
	if err != nil {
		return nil, err
	}

	// create provider
	provider := &SSHProvider{
		Config: config,
		Log:    logs,
	}

	return provider, nil
}

// getClient returns the appropriate SSH client (Go or Shell).
func (provider *SSHProvider) getClient() (SSHClient, error) {
	// If already have a client, return it
	if provider.client != nil {
		return provider.client, nil
	}

	// Check for legacy USE_BUILTIN_SSH option
	if provider.Config.UseBuiltinSSH {
		provider.Log.Debug("using legacy builtin ssh")
		provider.client = NewShellSSHClient(provider.Config, provider.Log)
		return provider.client, nil
	}

	// Try Go SSH (default)
	goClient := NewGoSSHClient(provider.Config, provider.Log)
	if err := goClient.Connect(); err != nil {
		_ = goClient.Close() // Clean up any partial state
		if shouldFallback(err) {
			provider.Log.Warnf("go ssh connection failed (fallback-eligible), falling back to shell: %v", err)
			provider.client = NewShellSSHClient(provider.Config, provider.Log)
			if err := provider.client.Connect(); err != nil {
				return nil, err
			}
			return provider.client, nil
		}
		return nil, err
	}

	provider.Log.Debug("using pure go ssh client")
	provider.client = goClient
	return provider.client, nil
}

func returnSSHError(provider *SSHProvider, command string) error {
	sshError := "Make sure you have configured the correct SSH host\n" +
		"and the following command can be executed on your system:\n"
	return fmt.Errorf("%s ssh %s %s", sshError, provider.Config.Host, command)
}

func execSSHCommand(provider *SSHProvider, command string, output io.Writer) error {
	client, err := provider.getClient()
	if err != nil {
		return err
	}

	return client.Execute(command, output)
}

func Init(provider *SSHProvider) error {
	if err := checkSSHOutput(provider); err != nil {
		return err
	}

	if err := checkLinuxServer(provider); err != nil {
		return err
	}

	if isRoot(provider) {
		return nil
	}

	if err := checkAgentPath(provider); err != nil {
		return err
	}

	return checkDockerAccess(provider)
}

func checkSSHOutput(provider *SSHProvider) error {
	out := new(bytes.Buffer)
	err := execSSHCommand(provider, "echo Devpod Test", out)
	if err != nil {
		return returnSSHError(provider, "echo Devpod Test")
	}
	if strings.TrimSpace(out.String()) != "Devpod Test" {
		return fmt.Errorf("ssh output mismatch")
	}
	return nil
}

func checkLinuxServer(provider *SSHProvider) error {
	out := new(bytes.Buffer)
	err := execSSHCommand(provider, "uname", out)
	if err != nil {
		return returnSSHError(provider, "uname")
	}
	if out.String() != "Linux\n" {
		_, _ = fmt.Fprintf(os.Stderr, "%s\n", out.String())
		return fmt.Errorf("ssh provider only works on linux servers")
	}
	return nil
}

func isRoot(provider *SSHProvider) bool {
	out := new(bytes.Buffer)
	err := execSSHCommand(provider, "id -ru", out)
	if err != nil {
		return false
	}
	return out.String() == "0\n"
}

func checkAgentPath(provider *SSHProvider) error {
	out := new(bytes.Buffer)
	agentDir := path.Dir(provider.Config.AgentPath)
	err1 := execSSHCommand(provider, "mkdir -p "+agentDir, out)
	err2 := execSSHCommand(provider, "test -w "+agentDir, out)
	if err1 != nil || err2 != nil {
		err := execSSHCommand(provider, "sudo -nl", out)
		if err != nil {
			return fmt.Errorf("%s is not writable, passwordless sudo or root user required", agentDir)
		}
	}
	return nil
}

func checkDockerAccess(provider *SSHProvider) error {
	out := new(bytes.Buffer)
	err := execSSHCommand(provider, provider.Config.DockerPath+" ps", out)
	if err != nil {
		err = execSSHCommand(provider, "sudo -nl", out)
		if err != nil {
			return fmt.Errorf(
				"%s not found, passwordless sudo or root user required. "+
					"if using another user please add to the docker group",
				provider.Config.DockerPath)
		}
	}
	return nil
}

func Command(provider *SSHProvider, command string) error {
	return execSSHCommand(provider, command, os.Stdout)
}
