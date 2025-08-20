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
	DefaultSSHPort uint = 22
)

type SSHProvider struct {
	Config           *options.Options
	Log              log.Logger
	WorkingDirectory string
}

func NewProvider(logs log.Logger) (*SSHProvider, error) {
	config, err := options.LoadDefault()
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
	if provider != nil && provider.Config != nil {
		_ = options.NormalizeSource().Apply(provider.Config)
	}

	remoteSSHPort, err := getSSHPortOrDefault(provider.Config.Port)
	if err != nil {
		return nil, fmt.Errorf("resolve port: %w", err)
	}

	cfg := loadSSHConfigIfAvailable(provider, provider.Config.Host)

	identityCandidates := resolveIdentityCandidates(cfg, provider.Config.Host)
	auth, err := buildAuth(identityCandidates)
	if err != nil {
		return nil, err
	}

	remoteAddr, err := resolveRemoteAddr(cfg, provider.Config.Host, provider.Config.Host)
	if err != nil {
		return nil, err
	}

	remoteUser, err := resolveRemoteUser(cfg, provider.Config.Host, provider.Config.User)
	if err != nil {
		return nil, err
	}

	hostKeyCB, err := createHostKeyVerificationCallback(provider)
	if err != nil {
		return nil, fmt.Errorf("known hosts: %w", err)
	}

	log.Default.Infof("Creating SSH client for %s@%s:%d", remoteUser, remoteAddr, remoteSSHPort)

	return goph.NewConn(&goph.Config{
		Auth:     auth,
		User:     remoteUser,
		Addr:     remoteAddr,
		Port:     remoteSSHPort,
		Callback: hostKeyCB,
	})
}

func SSHExec(provider *SSHProvider, command string) ([]byte, error) {
	log.Default.Infof("Executing command: %s", command)
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
	if provider != nil && provider.Config != nil {
		_ = options.NormalizeSource().Apply(provider.Config)
	}

	client, err := SSHClient(provider)
	if err != nil {
		return fmt.Errorf("failed to create ssh client: %w", err)
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
	if c, err := getSSHHostConfiguration(host); err == nil {
		return c
	} else {
		provider.Log.Debugf("ssh -G %q failed: %v (falling back to explicit config)", host, err)
	}
	return nil
}

func resolveIdentityCandidates(cfg *ssh_config.Config, host string) []string {
	var identityCandidates []string
	if cfg != nil {
		if id, _ := cfg.Get(host, SSHIdentityFile.String()); id != "" {
			files := strings.Fields(id)
			identityCandidates = append(identityCandidates, files...)
		}
	}
	return identityCandidates
}
