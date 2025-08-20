package ssh

import (
	"fmt"
	"strings"

	"github.com/kevinburke/ssh_config"
	"github.com/loft-sh/devpod-provider-ssh/pkg/options"
	"github.com/loft-sh/devpod-provider-ssh/pkg/util"
	"github.com/loft-sh/devpod/pkg/log"
	"github.com/melbahja/goph"
)

const (
	DefaultSSHPort      = "22"
	DefaultIdentityFile = "~/.ssh/id_ed25519"
)

type SSHProvider struct {
	Config           *options.Options
	Log              log.Logger
	WorkingDirectory string
}

func NewProvider(logs log.Logger) (*SSHProvider, error) {
	config, err := options.FromEnv()
	if err != nil {
		return nil, err
	}

	provider := &SSHProvider{
		Config: config,
		Log:    logs,
	}

	return provider, nil
}

func SSHClient(provider *SSHProvider) (*goph.Client, error) {
	host := provider.Config.Host

	remoteSSHPort, err := getSSHPortOrDefault(provider.Config.Port)
	if err != nil {
		return nil, fmt.Errorf("resolve port: %w", err)
	}

	cfg := loadSSHConfigIfAvailable(provider, host)

	identityCandidates := resolveIdentityCandidates(cfg, host)
	auth, err := buildAuth(identityCandidates)
	if err != nil {
		return nil, err
	}

	remoteAddr, err := resolveRemoteAddr(provider, cfg, host)
	if err != nil {
		return nil, err
	}

	remoteUser, err := resolveRemoteUser(provider, cfg, host)
	if err != nil {
		return nil, err
	}

	hostKeyCB, err := createHostKeyVerificationCallback(provider)
	if err != nil {
		return nil, fmt.Errorf("known hosts: %w", err)
	}

	return goph.NewConn(&goph.Config{
		Auth:     auth,
		User:     remoteUser,
		Addr:     remoteAddr,
		Port:     remoteSSHPort,
		Callback: hostKeyCB,
	})
}

func SSHExec(provider *SSHProvider, command string) ([]byte, error) {
	client, err := SSHClient(provider)
	if err != nil {
		return nil, fmt.Errorf("failed to create SSH client: %w", err)
	}
	defer client.Close()
	out, err := client.Run(command)
	if err != nil {
		return nil, fmt.Errorf("command execution failed: %w", err)
	}
	return out, nil
}

func ValidateRemoteHostConnection(provider *SSHProvider) error {
	options.OverrideSystemDefaults(provider.Config)

	client, err := SSHClient(provider)
	if err != nil {
		return fmt.Errorf("create ssh client: %w", err)
	}
	defer client.Close()

	remoteOS, err := resolveOperatingSystemType(client)
	if err != nil {
		return fmt.Errorf("detect OS: %w", err)
	}
	provider.Log.Infof("Detected remote OS: %s", remoteOS)

	switch remoteOS {
	case OSLinux:
		return validateLinuxHostConnection(provider, client)
	case OSWindows:
		return validateWindowsHostConnection(provider, client)
	default:
		return fmt.Errorf("unsupported or unknown remote OS")
	}
}

func validateLinuxHostConnection(provider *SSHProvider, client *goph.Client) error {
	cmds := []string{
		"uname -s",
		"lsb_release -is || true",
	}
	if provider.Config.DockerPath != "" {
		cmds = append(cmds, fmt.Sprintf("%s ps -qa", provider.Config.DockerPath))
	}

	for _, cmd := range cmds {
		out, err := client.Run(cmd)
		if err != nil {
			provider.Log.Errorf("Failed: %s: %v", cmd, err)
			continue
		}
		provider.Log.Infof("Output: %s", util.TrimWhitespace(string(out)))
	}
	return nil
}

func validateWindowsHostConnection(provider *SSHProvider, client *goph.Client) error {
	cmds := []string{
		`cmd /c "ver"`,
		`powershell -NoProfile -Command "(Get-CimInstance -ClassName Win32_OperatingSystem).Caption"`,
	}
	if provider.Config.DockerPath != "" {
		cmds = append(cmds, fmt.Sprintf("\"%s\" ps -qa", provider.Config.DockerPath))
	}

	for _, cmd := range cmds {
		out, err := client.Run(cmd)
		if err != nil {
			provider.Log.Errorf("Failed: %s: %v", cmd, err)
			continue
		}
		provider.Log.Infof("Output: %s", util.TrimWhitespace(string(out)))
	}
	return nil
}

func loadSSHConfigIfAvailable(provider *SSHProvider, host string) *ssh_config.Config {
	if host == "" {
		return nil
	}
	if c, err := getSshHostConfiguration(host); err == nil {
		return c
	} else {
		provider.Log.Debugf("ssh -G %q failed: %v (falling back to explicit config)", host, err)
	}
	return nil
}

func resolveIdentityCandidates(cfg *ssh_config.Config, host string) []string {
	var identityCandidates []string
	if cfg != nil {
		if id, _ := cfg.Get(host, SshIdentityFile.String()); id != "" {
			files := strings.Fields(id)
			identityCandidates = append(identityCandidates, files...)
		}
	}
	identityCandidates = append(identityCandidates, DefaultIdentityFile)
	return identityCandidates
}
