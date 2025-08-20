package options

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/loft-sh/devpod-provider-ssh/pkg/util"
)

const DefaultSSHPort = "22"

const (
	DOCKER_PATH        = "DOCKER_PATH"
	AGENT_PATH         = "AGENT_PATH"
	HOST               = "HOST"
	PORT               = "PORT"
	EXTRA_FLAGS        = "EXTRA_FLAGS"
	USE_BUILTIN_SSH    = "USE_BUILTIN_SSH"
	KNOWN_HOSTS_POLICY = "KNOWN_HOSTS_POLICY"
	KNOWN_HOSTS_PATH   = "KNOWN_HOSTS_PATH"
)

type KnownHostsPolicy string

const (
	KnownHostsStrict    KnownHostsPolicy = "strict"     // fail on unknown or mismatched keys
	KnownHostsAcceptNew KnownHostsPolicy = "accept-new" // add unknown hosts automatically
	KnownHostsIgnore    KnownHostsPolicy = "ignore"     // do not verify host keys (insecure)
)

type Options struct {
	DockerPath       string
	AgentPath        string
	User             string
	Host             string
	Port             string
	ExtraFlags       string
	UseBuiltinSSH    bool
	KnownHostsPolicy KnownHostsPolicy
	KnownHostsPath   string
}

type Source interface {
	Apply(*Options) error
}

func Load(sources ...Source) (*Options, error) {
	o := &Options{}
	for _, s := range sources {
		if err := s.Apply(o); err != nil {
			return nil, err
		}
	}
	return o, nil
}

// Provides sane OS defaults.
func DefaultsSource() Source { return defaultsSource{} }

type defaultsSource struct{}

func (defaultsSource) Apply(o *Options) error {
	curUser, err := user.Current()
	if err != nil {
		return fmt.Errorf("resolve current user: %w", err)
	}
	host, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("resolve hostname: %w", err)
	}
	goos := runtime.GOOS

	defaultKnownHostsPath := func() string {
		if home, err := os2UserHomeDir(); err == nil {
			return filepath.Join(home, ".ssh", "known_hosts")
		}
		return ""
	}()

	// OS-specific defaults
	switch goos {
	case "linux", "darwin":
		if o.DockerPath == "" {
			o.DockerPath = "/usr/bin/docker"
		}
		if o.AgentPath == "" {
			o.AgentPath = "/usr/bin/ssh-agent"
		}
	case "windows":
		if o.DockerPath == "" {
			o.DockerPath = "C:\\Program Files\\Docker\\Docker\\resources\\bin\\docker.exe"
		}
		if o.AgentPath == "" {
			o.AgentPath = "C:\\Windows\\System32\\OpenSSH\\ssh-agent.exe"
		}
	}

	if o.User == "" {
		o.User = curUser.Username
	}
	if o.Host == "" {
		o.Host = host
	}
	if o.Port == "" {
		o.Port = DefaultSSHPort
	}
	if o.KnownHostsPolicy == "" {
		o.KnownHostsPolicy = KnownHostsStrict
	}
	if o.KnownHostsPath == "" {
		o.KnownHostsPath = defaultKnownHostsPath
	}
	return nil
}

// Reads configuration from environment variables; missing vars are ignored.
func EnvSource() Source { return envSource{} }

type envSource struct{}

func (envSource) Apply(o *Options) error {
	if v := os.Getenv(DOCKER_PATH); v != "" {
		o.DockerPath = v
	}
	if v := os.Getenv(AGENT_PATH); v != "" {
		o.AgentPath = v
	}
	if v := os.Getenv(HOST); v != "" {
		o.Host = v
	}
	if v := os.Getenv(PORT); v != "" {
		o.Port = strings.TrimSpace(v)
	}
	if v := os.Getenv(EXTRA_FLAGS); v != "" {
		o.ExtraFlags = v
	}
	if v := os.Getenv(USE_BUILTIN_SSH); v != "" {
		o.UseBuiltinSSH = strings.EqualFold(strings.TrimSpace(v), "true")
	}
	if v := os.Getenv(KNOWN_HOSTS_POLICY); v != "" {
		o.KnownHostsPolicy = ParseKnownHostsPolicy(v)
	}
	if v := os.Getenv(KNOWN_HOSTS_PATH); v != "" {
		o.KnownHostsPath = v
	}
	return nil
}

func NormalizeSource() Source { return normalizeSource{} }

type normalizeSource struct{}

func (normalizeSource) Apply(o *Options) error {
	if o.KnownHostsPath != "" {
		if p, err := util.ResolveHomeDirToAbs(o.KnownHostsPath); err == nil && p != "" {
			o.KnownHostsPath = p
		}
	}
	return nil
}

// Loads (1) defaults, (2) env and (3) normalized values in that order,
// overriding previous source values (e.g., environment overrides defaults).
func LoadDefault() (*Options, error) {
	return Load(DefaultsSource(), EnvSource(), NormalizeSource())
}

// os.UserHomeDir can fail; wrap to allow reuse in inline funcs
func os2UserHomeDir() (string, error) {
	return os.UserHomeDir()
}

func ParseKnownHostsPolicy(v string) KnownHostsPolicy {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "ignore", "insecure":
		return KnownHostsIgnore
	case "accept-new", "add-unknown":
		return KnownHostsAcceptNew
	default:
		return KnownHostsStrict
	}
}
