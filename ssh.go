package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/kevinburke/ssh_config"
	"github.com/loft-sh/devpod-provider-ssh/pkg/options"
	"github.com/loft-sh/devpod/pkg/log"
	"github.com/melbahja/goph"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

type OperatingSystem int

const (
	DefaultSSHPort      = "22"
	DefaultIdentityFile = "~/.ssh/id_ed25519"
)

const (
	OSLinux OperatingSystem = iota
	OSWindows
	OSMac
	OSUnknown
)

var OperatingSystems = map[OperatingSystem]string{
	OSLinux:   "Linux",
	OSWindows: "Windows",
	OSMac:     "macOS",
	OSUnknown: "Unknown",
}

var IdentityFileProviders = &[]string{
	"~/.ssh/id_ed25519",
	"~/.ssh/id_ecdsa",
	"~/.ssh/id_rsa",
}

func (os OperatingSystem) String() string {
	if name, ok := OperatingSystems[os]; ok {
		return name
	}
	return OperatingSystems[OSUnknown]
}

type SshHostConfigKey int

const (
	SshHostConfigKeyHostname SshHostConfigKey = iota
	SshHostConfigKeyUser
	SshIdentityFile
)

var SshHostConfigKeyMap = map[SshHostConfigKey]string{
	SshHostConfigKeyHostname: "Hostname",
	SshHostConfigKeyUser:     "User",
	SshIdentityFile:          "IdentityFile",
}

func (hk SshHostConfigKey) String() string {
	return SshHostConfigKeyMap[hk]
}

var Overrides *options.Options = &options.Options{
	DockerPath: "C:\\Program Files\\Docker\\Docker\\resources\\bin\\docker.exe",
	AgentPath:  "C:\\Windows\\System32\\OpenSSH\\ssh-agent.exe",
	// AgentPath:     "/usr/bin/ssh-agent",
	// User: "ocean_trader",
	Host: "windows",
	// Host: "vps1",
	// Port: "4422",
	// ExtraFlags:    "",
	// UseBuiltinSSH: false,
}

type SSHProvider struct {
	Config           *options.Options
	Log              log.Logger
	WorkingDirectory string
}

var DefaultProvider *SSHProvider = &SSHProvider{
	Config:           Overrides,
	Log:              log.Default,
	WorkingDirectory: "/home/user",
}

func trimWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func resolveOperatingSystemType(client *goph.Client) (OperatingSystem, error) {
	out, err := client.Run("uname -s")
	if err == nil {
		s := strings.ToLower(strings.TrimSpace(string(out)))
		switch {
		case strings.Contains(s, "linux"):
			return OSLinux, nil
		case strings.Contains(s, "darwin"):
			return OSMac, nil
		}
	}

	// Windows probes
	if out, err = client.Run(`cmd /c "ver"`); err == nil {
		if strings.Contains(strings.ToLower(string(out)), "windows") {
			return OSWindows, nil
		}
	}
	if out, err = client.Run(`powershell -NoProfile -Command "[System.Environment]::OSVersion.VersionString"`); err == nil {
		if strings.Contains(strings.ToLower(string(out)), "windows") {
			return OSWindows, nil
		}
	}

	return OSUnknown, fmt.Errorf("could not determine remote OS")
}

func buildAuth(identityCandidates []string) (goph.Auth, error) {
	// Prefer ssh-agent if available
	if os.Getenv("SSH_AUTH_SOCK") != "" {
		if a, err := goph.UseAgent(); err == nil {
			return a, nil
		} else {
			log.Default.Debugf("SSH agent not usable: %v", err)
		}
	}
	for _, f := range identityCandidates {
		path, err := getIdentityFile(f)
		if err != nil || path == "" {
			if err != nil {
				log.Default.Debugf("Identity candidate skipped %s: %v", f, err)
			}
			continue
		}
		if auth, err := goph.Key(path, ""); err == nil {
			return auth, nil
		} else {
			log.Default.Debugf("Key not usable %s: %v", path, err)
		}
	}
	return nil, fmt.Errorf("no usable SSH auth found")
}

func getSSHPortOrDefault(portStr string) (uint, error) {
	portStr = strings.TrimSpace(portStr)
	if portStr == "" {
		p, err := strconv.ParseUint(DefaultSSHPort, 10, 16)
		if err != nil {
			return 0, fmt.Errorf("failed to parse default SSH port: %w", err)
		}
		return uint(p), nil
	}
	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil || port == 0 {
		p, err := strconv.ParseUint(DefaultSSHPort, 10, 16)
		if err != nil {
			return 0, fmt.Errorf("failed to parse default SSH port: %w", err)
		}
		log.Default.Warnf("Invalid port %s. Falling back to default SSH port: %d", portStr, p)
		return uint(p), nil
	}
	return uint(port), nil
}

// parseExtraFlagsSet normalizes EXTRA_FLAGS into a set for O(1) checks.
// Flags can be comma or whitespace separated, case-insensitive.
func parseExtraFlagsSet(flags string) map[string]bool {
	set := map[string]bool{}
	if flags == "" {
		return set
	}
	f := func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' }
	for _, t := range strings.FieldsFunc(flags, f) {
		t = strings.TrimSpace(strings.ToLower(t))
		if t != "" {
			set[t] = true
		}
	}
	return set
}

func SSHClient(provider *SSHProvider) (*goph.Client, error) {
	host := provider.Config.Host
	remoteSSHPort, err := getSSHPortOrDefault(provider.Config.Port)
	if err != nil {
		return nil, fmt.Errorf("resolve port: %w", err)
	}

	var cfg *ssh_config.Config
	if host != "" {
		if c, err := getSshHostConfiguration(host); err == nil {
			cfg = c
		} else {
			provider.Log.Debugf("ssh -G %q failed: %v (falling back to explicit config)", host, err)
		}
	}

	// Resolve identity candidates: ssh config IdentityFile (may be multiple), then default
	// ssh -G IdentityFile may contain multiple paths
	var identityCandidates []string
	if cfg != nil {
		if id, _ := cfg.Get(host, SshIdentityFile.String()); id != "" {
			files := strings.Fields(id)
			identityCandidates = append(identityCandidates, files...)
		}
	}
	identityCandidates = append(identityCandidates, DefaultIdentityFile)

	auth, err := buildAuth(identityCandidates)
	if err != nil {
		return nil, err
	}

	remoteAddr := provider.Config.Host
	if cfg != nil {
		if v, _ := cfg.Get(host, SshHostConfigKeyHostname.String()); v != "" {
			remoteAddr = v
		}
	}
	if remoteAddr == "" {
		return nil, fmt.Errorf("no remote address provided (Host or ssh config Hostname required)")
	}

	remoteUser := provider.Config.User
	if cfg != nil {
		if v, _ := cfg.Get(host, SshHostConfigKeyUser.String()); v != "" {
			remoteUser = v
		}
	}
	if remoteUser == "" {
		return nil, fmt.Errorf("no remote user provided (User or ssh config User required)")
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

func initialize(provider *SSHProvider) error {
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

	linuxCommands := []string{
		"uname -s",
		"lsb_release -is || true",
	}
	if provider.Config.DockerPath != "" {
		linuxCommands = append(linuxCommands, fmt.Sprintf("%s ps -qa", provider.Config.DockerPath))
	}

	windowsCommands := []string{
		`cmd /c "ver"`,
		`powershell -NoProfile -Command "(Get-CimInstance -ClassName Win32_OperatingSystem).Caption"`,
	}
	if provider.Config.DockerPath != "" {
		windowsCommands = append(windowsCommands, fmt.Sprintf("\"%s\" ps -qa", provider.Config.DockerPath))
	}

	switch remoteOS {
	case OSLinux:
		provider.Log.Infof("Running initialization commands for Linux")
		for _, cmd := range linuxCommands {
			out, err := client.Run(cmd)
			if err != nil {
				provider.Log.Errorf("Failed: %s: %v", cmd, err)
				continue
			}
			provider.Log.Infof("Output: %s", trimWhitespace(string(out)))
		}
	case OSWindows:
		provider.Log.Infof("Running initialization commands for Windows")
		for _, cmd := range windowsCommands {
			out, err := client.Run(cmd)
			if err != nil {
				provider.Log.Errorf("Failed: %s: %v", cmd, err)
				continue
			}
			provider.Log.Infof("Output: %s", trimWhitespace(string(out)))
		}
	default:
		return fmt.Errorf("unsupported or unknown remote OS")
	}
	return nil
}

func addUnknownHostsCallback(host string, remote net.Addr, key ssh.PublicKey) error {
	hostFound, err := goph.CheckKnownHost(host, remote, key, "")

	// Host in known hosts but key mismatch => potential MITM
	if hostFound && err != nil {
		log.Default.Warnf("Host key mismatch for %s: %v", host, err)
		return err
	}

	// If the host is not found in known_hosts, add it
	if !hostFound && err != nil {
		var ke *knownhosts.KeyError
		if errors.As(err, &ke) && (ke == nil || len(ke.Want) == 0) {
			log.Default.Warnf("Host %s is not in known_hosts, adding it", host)
			if err := goph.AddKnownHost(host, remote, key, ""); err != nil {
				return fmt.Errorf("failed to add host %s to known_hosts: %w", host, err)
			}
			log.Default.Infof("Host %s added to known_hosts", host)
			return nil
		}
		return err
	}
	return nil
}

func createHostKeyVerificationCallback(provider *SSHProvider) (ssh.HostKeyCallback, error) {
	flags := parseExtraFlagsSet(provider.Config.ExtraFlags)
	if flags["addunknownhosts"] || flags["addunknownhost"] {
		return addUnknownHostsCallback, nil
	}
	if flags["ignoreknownhosts"] || flags["strict_host_key_checking=no"] {
		return ssh.InsecureIgnoreHostKey(), nil
	}
	callbackFn, err := goph.DefaultKnownHosts()
	if err != nil {
		return nil, fmt.Errorf("load known_hosts: %w", err)
	}
	return callbackFn, nil
}

func resolveHomeDirToAbs(path string) (string, error) {
	if strings.HasPrefix(path, "~") {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get user home directory: %w", err)
		}
		return strings.Replace(path, "~", homeDir, 1), nil
	}
	return path, nil
}

func getIdentityFile(file string) (string, error) {
	file, err := resolveHomeDirToAbs(file)
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	if file != "" {
		if st, err := os.Stat(file); err == nil && !st.IsDir() {
			return file, nil
		}
	}
	for _, defaultFile := range *IdentityFileProviders {
		defaultFile, err = resolveHomeDirToAbs(defaultFile)
		if err != nil {
			return "", fmt.Errorf("resolve default identity file: %w", err)
		}
		if st, err := os.Stat(defaultFile); err == nil && !st.IsDir() {
			log.Default.Infof("Using default identity file: %s", defaultFile)
			return defaultFile, nil
		}
		log.Default.Debugf("Default identity file does not exist: %s", defaultFile)
	}
	return "", fmt.Errorf("no identity file found")
}

func getSshHostConfiguration(host string) (*ssh_config.Config, error) {
	bytes, err := exec.Command("ssh", "-G", host).Output()
	if err != nil {
		return nil, err
	}
	return ssh_config.Decode(strings.NewReader(string(bytes)))
}

func main() {
	// Init(DefaultProvider)
	initialize(DefaultProvider)
}
