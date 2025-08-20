package ssh

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
	"github.com/loft-sh/devpod-provider-ssh/pkg/util"
	"github.com/loft-sh/devpod/pkg/log"
	"github.com/melbahja/goph"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

type OperatingSystem int

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

var IdentityFileProviders = &[]string{
	"~/.ssh/id_ed25519",
	"~/.ssh/id_ecdsa",
	"~/.ssh/id_rsa",
}

func (hk SshHostConfigKey) String() string {
	return SshHostConfigKeyMap[hk]
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
	switch provider.Config.KnownHostsPolicy {
	case options.KnownHostsIgnore:
		return ssh.InsecureIgnoreHostKey(), nil
	case options.KnownHostsAcceptNew:
		return addUnknownHostsCallback, nil
	default:
		// KnownHostsStrict: load from the configured path (if provided) or default
		if provider.Config.KnownHostsPath != "" {
			cb, err := knownhosts.New(provider.Config.KnownHostsPath)
			if err != nil {
				return nil, fmt.Errorf("load known_hosts from %s: %w", provider.Config.KnownHostsPath, err)
			}
			return cb, nil
		}
		callbackFn, err := goph.DefaultKnownHosts()
		if err != nil {
			return nil, fmt.Errorf("load known_hosts: %w", err)
		}
		return callbackFn, nil
	}
}

func getIdentityFile(file string) (string, error) {
	file, err := util.ResolveHomeDirToAbs(file)
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	if file != "" {
		if st, err := os.Stat(file); err == nil && !st.IsDir() {
			return file, nil
		}
	}
	for _, defaultFile := range *IdentityFileProviders {
		defaultFile, err = util.ResolveHomeDirToAbs(defaultFile)
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

type remoteConfigResolver struct {
	provider     *SSHProvider
	cfg          *ssh_config.Config
	sshKeyLookup string
}

func (r *remoteConfigResolver) resolve(key SshHostConfigKey, defaultVal string, required bool) (string, error) {
	val := defaultVal
	if r.cfg != nil {
		if v, _ := r.cfg.Get(r.sshKeyLookup, key.String()); v != "" {
			val = v
		}
	}
	if val == "" && required {
		return "", fmt.Errorf("missing required SSH config key %q for lookup %q", key.String(), r.sshKeyLookup)
	}
	return val, nil
}

func newRemoteConfigResolver(provider *SSHProvider, cfg *ssh_config.Config, lookup string) *remoteConfigResolver {
	return &remoteConfigResolver{
		provider:     provider,
		cfg:          cfg,
		sshKeyLookup: lookup,
	}
}

func resolveRemoteAddr(provider *SSHProvider, cfg *ssh_config.Config, host string) (string, error) {
	resolver := newRemoteConfigResolver(provider, cfg, host)
	return resolver.resolve(SshHostConfigKeyHostname, provider.Config.Host, true)
}

func resolveRemoteUser(provider *SSHProvider, cfg *ssh_config.Config, host string) (string, error) {
	resolver := newRemoteConfigResolver(provider, cfg, host)
	return resolver.resolve(SshHostConfigKeyUser, provider.Config.User, true)
}
