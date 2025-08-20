package options

import (
	"fmt"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
)

const DefaultSSHPort = "22"

var (
	DOCKER_PATH     = "DOCKER_PATH"
	AGENT_PATH      = "AGENT_PATH"
	HOST            = "HOST"
	PORT            = "PORT"
	EXTRA_FLAGS     = "EXTRA_FLAGS"
	USE_BUILTIN_SSH = "USE_BUILTIN_SSH"
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
	DockerPath    string
	AgentPath     string
	User          string
	Host          string
	Port          string
	ExtraFlags    string
	UseBuiltinSSH bool
	KnownHostsPolicy KnownHostsPolicy
	KnownHostsPath   string
}

func FromEnv() (*Options, error) {
	retOptions := &Options{}

	var err error

	retOptions.DockerPath, err = fromEnvOrError(DOCKER_PATH)
	if err != nil {
		return nil, err
	}

	retOptions.AgentPath, err = fromEnvOrError(AGENT_PATH)
	if err != nil {
		return nil, err
	}

	retOptions.ExtraFlags = os.Getenv(EXTRA_FLAGS)

	retOptions.Host, err = fromEnvOrError(HOST)
	if err != nil {
		return nil, err
	}

	retOptions.Port, err = fromEnvOrError(PORT)
	if err != nil {
		return nil, err
	}

	builtinSSH, err := fromEnvOrError(USE_BUILTIN_SSH)
	if err != nil {
		return nil, err
	}
	retOptions.UseBuiltinSSH = builtinSSH == "true"

	if p := os.Getenv(KNOWN_HOSTS_POLICY); p != "" {
		retOptions.KnownHostsPolicy = ParseKnownHostsPolicy(p)
	}
	if p := os.Getenv(KNOWN_HOSTS_PATH); p != "" {
		retOptions.KnownHostsPath = p
	}

	return retOptions, nil
}

func fromEnvOrError(name string) (string, error) {
	val := os.Getenv(name)
	if val == "" {
		return "", fmt.Errorf(
			"couldn't find option %s in environment, please make sure %s is defined",
			name,
			name,
		)
	}

	return val, nil
}

func getDefaultOptionsForOS() *Options {
	user, err := user.Current()
	if err != nil {
		log.Fatalf("Failed to get current user: %v", err)
	}
	host, err := os.Hostname()
	if err != nil {
		log.Fatalf("Failed to get hostname: %v", err)
	}
	os := runtime.GOOS

	defaultKnownHostsPath := func() string {
		if home, err := os2UserHomeDir(); err == nil {
			return filepath.Join(home, ".ssh", "known_hosts")
		}
		return ""
	}()

	if os == "linux" || os == "darwin" {
		return &Options{
			DockerPath:    "/usr/bin/docker",
			AgentPath:     "/usr/bin/ssh-agent",
			User:          user.Username,
			Host:          host,
			Port:          DefaultSSHPort,
			ExtraFlags:    "",
			UseBuiltinSSH: false,
			KnownHostsPolicy: KnownHostsStrict,
			KnownHostsPath:   defaultKnownHostsPath,
		}
	}

	if os == "windows" {
		return &Options{
			DockerPath:    "C:\\Program Files\\Docker\\Docker\\resources\\bin\\docker.exe",
			AgentPath:     "C:\\Windows\\System32\\OpenSSH\\ssh-agent.exe",
			User:          user.Username,
			Host:          host,
			Port:          DefaultSSHPort,
			ExtraFlags:    "",
			UseBuiltinSSH: false,
			KnownHostsPolicy: KnownHostsStrict,
			KnownHostsPath:   defaultKnownHostsPath,
		}
	}

	return nil
}

func OverrideSystemDefaults(opts *Options) {
	defaultOpts := getDefaultOptionsForOS()
	if defaultOpts == nil {
		log.Fatalf("Unsupported operating system: %s", runtime.GOOS)
	}

	if opts == nil {
		return
	}

	if opts.DockerPath == "" {
		opts.DockerPath = defaultOpts.DockerPath
	}
	if opts.AgentPath == "" {
		opts.AgentPath = defaultOpts.AgentPath
	}
	if opts.User == "" {
		opts.User = defaultOpts.User
	}
	if opts.Host == "" {
		opts.Host = defaultOpts.Host
	}
	if opts.Port == "" {
		opts.Port = defaultOpts.Port
	}
	if opts.ExtraFlags == "" {
		opts.ExtraFlags = defaultOpts.ExtraFlags
	}
	if !opts.UseBuiltinSSH && defaultOpts.UseBuiltinSSH {
		opts.UseBuiltinSSH = true
	}
	if opts.KnownHostsPolicy == "" {
		opts.KnownHostsPolicy = defaultOpts.KnownHostsPolicy
	}
	if opts.KnownHostsPath == "" {
		opts.KnownHostsPath = defaultOpts.KnownHostsPath
	}
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
