package ssh

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
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

func (o OperatingSystem) String() string {
	switch o {
	case OSLinux:
		return "Linux"
	case OSWindows:
		return "Windows"
	case OSMac:
		return "macOS"
	default:
		return "Unknown"
	}
}

type SSHHostConfigKey int

const (
	SSHHostConfigKeyHostname SSHHostConfigKey = iota
	SSHHostConfigKeyUser
	SSHIdentityFile
)

var SSHHostConfigKeyMap = map[SSHHostConfigKey]string{
	SSHHostConfigKeyHostname: "Hostname",
	SSHHostConfigKeyUser:     "User",
	SSHIdentityFile:          "IdentityFile",
}

func (hk SSHHostConfigKey) String() string {
	return SSHHostConfigKeyMap[hk]
}

func DefaultIdentityFiles() []string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}
	return []string{
		filepath.Join(home, ".ssh", "id_ed25519"),
		filepath.Join(home, ".ssh", "id_ecdsa"),
		filepath.Join(home, ".ssh", "id_rsa"),
	}
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
		if provider.Config.KnownHostsPath != "" {
			cb, err := knownhosts.New(provider.Config.KnownHostsPath)
			if err != nil {
				return nil, fmt.Errorf("load known_hosts from %s: %w", provider.Config.KnownHostsPath, err)
			}
			return func(host string, remote net.Addr, key ssh.PublicKey) error {
				if err := cb(host, remote, key); err != nil {
					var ke *knownhosts.KeyError
					if errors.As(err, &ke) && (ke == nil || len(ke.Want) == 0) {
						// Unknown -> add and accept
						if err := goph.AddKnownHost(host, remote, key, provider.Config.KnownHostsPath); err != nil {
							return fmt.Errorf("failed to add host %s to known_hosts: %w", host, err)
						}
						log.Default.Infof("Host %s added to known_hosts (%s)", host, provider.Config.KnownHostsPath)
						return nil
					}
					return err
				}
				return nil
			}, nil
		}
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
	return "", fmt.Errorf("identity file not found: %s", file)
}

func getSSHHostConfiguration(host string) (*ssh_config.Config, error) {
	out, err := exec.Command("ssh", "-G", host).Output()
	if err != nil {
		return nil, err
	}
	return ssh_config.Decode(bytes.NewReader(out))
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
	for _, f := range identityCandidates {
		path, err := util.ResolveHomeDirToAbs(f)
		if err != nil || path == "" {
			log.Default.Debugf("Identity candidate skipped %s: %v", f, err)
			continue
		}
		if st, err := os.Stat(path); err == nil && !st.IsDir() {
			if auth, err := goph.Key(path, ""); err == nil {
				return auth, nil
			} else {
				log.Default.Debugf("Key not usable %s: %v", path, err)
			}
		}
	}

	if os.Getenv("SSH_AUTH_SOCK") != "" {
		if a, err := goph.UseAgent(); err == nil {
			return a, nil
		} else {
			log.Default.Debugf("SSH agent not usable: %v", err)
		}
	}

	for _, path := range DefaultIdentityFiles() {
		if st, err := os.Stat(path); err == nil && !st.IsDir() {
			if auth, err := goph.Key(path, ""); err == nil {
				log.Default.Debugf("Using default identity file: %s", path)
				return auth, nil
			}
		}
	}
	return nil, fmt.Errorf("no usable SSH auth found")
}

func getSSHPortOrDefault(portStr string) (uint, error) {
	portStr = strings.TrimSpace(portStr)
	if portStr == "" {
		return DefaultSSHPort, nil
	}
	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil || port == 0 {
		log.Default.Warnf("Invalid port %s. Falling back to default SSH port: %d", portStr, DefaultSSHPort)
		return DefaultSSHPort, nil
	}
	return uint(port), nil
}

type remoteConfigResolver struct {
	cfg          *ssh_config.Config
	sshKeyLookup string
}

func (r *remoteConfigResolver) resolve(value string, defaultVal string) (string, error) {
	val := defaultVal
	if r.cfg != nil {
		if v, _ := r.cfg.Get(r.sshKeyLookup, value); v != "" {
			val = v
		}
	}
	if val == "" {
		return "", fmt.Errorf("missing SSH config key %q for lookup %q", value, r.sshKeyLookup)
	}
	return val, nil
}

func resolveRemoteAddr(cfg *ssh_config.Config, host string) (string, error) {
	resolver := &remoteConfigResolver{
		cfg:          cfg,
		sshKeyLookup: host,
	}
	return resolver.resolve(SSHHostConfigKeyHostname.String(), host)
}

func resolveRemoteUser(cfg *ssh_config.Config, user string) (string, error) {
	resolver := &remoteConfigResolver{
		cfg:          cfg,
		sshKeyLookup: user,
	}
	return resolver.resolve(SSHHostConfigKeyUser.String(), user)
}
