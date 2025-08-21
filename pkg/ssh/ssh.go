package ssh

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"

	"github.com/kevinburke/ssh_config"
	"github.com/loft-sh/devpod-provider-ssh/pkg/options"
	"github.com/loft-sh/devpod-provider-ssh/pkg/util"
	"github.com/loft-sh/log"
	"github.com/melbahja/goph"
)

const (
	DefaultSSHPort uint = 22
)

type SSHProvider struct {
	// Config holds the SSH connection options
	Config *options.Options
	// Log is the logger used for output
	Log log.Logger
	// WorkingDirectory is the working directory for SSH operations
	WorkingDirectory string
	// cached remote OS detection to avoid repeated probes when using ssh binary
	detectedOS OperatingSystem
	osDetected bool
}

// NewProvider returns a new SSHProvider with loaded configuration and logger
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

func SSHExec(provider *SSHProvider, command string) ([]byte, error) {
	if provider != nil && provider.Config.UseBuiltinSSH {
		provider.Log.Debugf("Executing command using Go SSH client: %s", command)
		return SSHExecGo(provider, command)
	}

	provider.Log.Debugf("Executing command using system SSH binary: %s", command)
	return SSHExecBinary(provider, command)
}

// SSHExecBinary executes a command on the remote host using the system ssh binary
func SSHExecBinary(provider *SSHProvider, command string) ([]byte, error) {
	if provider == nil || provider.Config == nil {
		return nil, fmt.Errorf("provider or config is nil")
	}

	// Detect remote OS via ssh binary and wrap with WSL if Windows + WSL distro provided
	if provider.Config.WSLDistro != "" {
		remoteOS, err := detectRemoteOSWithBinary(provider)
		if err != nil {
			provider.Log.Debugf("remote OS detection (binary) failed, proceeding without WSL wrap: %v", err)
		} else if remoteOS == OSWindows {
			command = wrapWSLCommand(provider.Config.WSLDistro, command)
			provider.Log.Debugf("WSL: %s", command)
		}
	}
	args := buildSSHBinaryArgs(provider)
	args = append(args, provider.Config.Host, command)

	var stdout, stderr bytes.Buffer
	cmd := exec.Command("ssh", args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return stdout.Bytes(), fmt.Errorf("ssh binary error: %w\nstderr: %s", err, stderr.String())
	}
	return stdout.Bytes(), nil
}

// SSHClient creates and returns a goph.Client for the given SSHProvider configuration
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

// SSHExec executes a command on the remote host using the Go-based SSH client
func SSHExecGo(provider *SSHProvider, command string) ([]byte, error) {
	log.Default.Infof("Preparing to execute command: %s", command)
	client, err := SSHClient(provider)
	if err != nil {
		return nil, fmt.Errorf("failed to create SSH client: %w", err)
	}
	defer client.Close()

	// Detect remote OS via SSH client and wrap with WSL if needed
	if provider.Config.WSLDistro != "" {
		if remoteOS, err := resolveOperatingSystemType(client); err == nil {
			if remoteOS == OSWindows {
				command = wrapWSLCommand(provider.Config.WSLDistro, command)
				provider.Log.Debugf("WSL: %s", command)
			}
		} else {
			provider.Log.Debugf("remote OS detection (go ssh) failed, proceeding without WSL wrap: %v", err)
		}
	}

	log.Default.Infof("Executing command: %s", command)
	out, err := client.Run(command)
	if err != nil {
		return nil, fmt.Errorf("command execution failed: %w", err)
	}
	return out, nil
}

func buildSSHBinaryArgs(provider *SSHProvider) []string {
	args := []string{"-oStrictHostKeyChecking=no", "-oBatchMode=yes"}
	if provider.Config.Port != "" && provider.Config.Port != "22" {
		args = append(args, "-p", provider.Config.Port)
	}
	if provider.Config.ExtraFlags != "" {
		extra := strings.Fields(provider.Config.ExtraFlags)
		args = append(args, extra...)
	}
	return args
}

func wrapWSLCommand(distro, command string) string {
	// Escape existing double quotes in the command to avoid breaking the -c string
	escaped := strings.ReplaceAll(command, "\"", "\\\"")
	return fmt.Sprintf("wsl.exe -d %s -- /bin/sh -lc \"%s\"", distro, escaped)
}

func detectRemoteOSWithBinary(provider *SSHProvider) (OperatingSystem, error) {
	if provider.osDetected {
		return provider.detectedOS, nil
	}

	runProbe := func(cmdStr string) (string, error) {
		var stdout, stderr bytes.Buffer
		args := buildSSHBinaryArgs(provider)
		args = append(args, provider.Config.Host, cmdStr)
		cmd := exec.Command("ssh", args...)
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("probe failed: %w; stderr: %s", err, stderr.String())
		}
		return stdout.String(), nil
	}

	// Linux/macOS probe
	if out, err := runProbe("uname -s"); err == nil {
		s := strings.ToLower(strings.TrimSpace(out))
		switch {
		case strings.Contains(s, "linux"):
			provider.detectedOS = OSLinux
			provider.osDetected = true
			return provider.detectedOS, nil
		case strings.Contains(s, "darwin"):
			provider.detectedOS = OSMac
			provider.osDetected = true
			return provider.detectedOS, nil
		}
	}

	// Windows probes
	if out, err := runProbe(`cmd /c "ver"`); err == nil {
		if strings.Contains(strings.ToLower(out), "windows") {
			provider.detectedOS = OSWindows
			provider.osDetected = true
			return provider.detectedOS, nil
		}
	}
	if out, err := runProbe(`powershell -NoProfile -Command "[System.Environment]::OSVersion.VersionString"`); err == nil {
		if strings.Contains(strings.ToLower(out), "windows") {
			provider.detectedOS = OSWindows
			provider.osDetected = true
			return provider.detectedOS, nil
		}
	}

	return OSUnknown, fmt.Errorf("could not determine remote OS via ssh binary")
}

// ValidateRemoteHostConnection checks connectivity and basic requirements on the remote host
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
	case OSMac:
		// macOS is Unix-like; reuse Linux validation where applicable
		return validateLinuxHostConnection(provider, client)
	case OSUnknown:
		return fmt.Errorf("unsupported or unknown remote OS")
	default:
		return fmt.Errorf("unsupported or unknown remote OS")
	}
}

func validateLinuxHostConnection(provider *SSHProvider, client *goph.Client) error {
	provider.Log.Debugf("Validating Linux host connection")
	cmds := []string{
		"uname -s",
		"lsb_release -is",
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
	provider.Log.Debugf("Validating Windows host connection")
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
