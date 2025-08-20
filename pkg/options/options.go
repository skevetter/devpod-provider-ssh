package options

import (
	"fmt"
	"log"
	"os"
	"os/user"
	"runtime"
)

const DefaultSSHPort = "22"

var (
	DOCKER_PATH     = "DOCKER_PATH"
	AGENT_PATH      = "AGENT_PATH"
	HOST            = "HOST"
	PORT            = "PORT"
	EXTRA_FLAGS     = "EXTRA_FLAGS"
	USE_BUILTIN_SSH = "USE_BUILTIN_SSH"
)

type Options struct {
	DockerPath    string
	AgentPath     string
	User          string
	Host          string
	Port          string
	ExtraFlags    string
	UseBuiltinSSH bool
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

	if os == "linux" || os == "darwin" {
		return &Options{
			DockerPath:    "/usr/bin/docker",
			AgentPath:     "/usr/bin/ssh-agent",
			User:          user.Username,
			Host:          host,
			Port:          DefaultSSHPort,
			ExtraFlags:    "",
			UseBuiltinSSH: false,
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
}
