package main

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/goccy/go-yaml"
)

const (
	providerName = "ssh"
	githubOwner  = "skevetter"
	githubRepo   = "devpod-provider-ssh"
)

type Provider struct {
	Name         string            `yaml:"name"`
	Version      string            `yaml:"version"`
	Description  string            `yaml:"description"`
	Icon         string            `yaml:"icon"`
	IconDark     string            `yaml:"iconDark"`
	OptionGroups []OptionGroup     `yaml:"optionGroups"`
	Options      Options           `yaml:"options"`
	Agent        Agent             `yaml:"agent"`
	Binaries     Binaries          `yaml:"binaries"`
	Exec         map[string]string `yaml:"exec"`
}

type OptionGroup struct {
	Name           string   `yaml:"name"`
	DefaultVisible bool     `yaml:"defaultVisible"`
	Options        []string `yaml:"options"`
}

type Options map[string]Option

type Option struct {
	Description string `yaml:"description,omitempty"`
	Required    bool   `yaml:"required,omitempty"`
	Default     string `yaml:"default,omitempty"`
	Type        string `yaml:"type,omitempty"`
	Command     string `yaml:"command,omitempty"`
}

type Agent struct {
	Path                    string       `yaml:"path"`
	InactivityTimeout       string       `yaml:"inactivityTimeout"`
	InjectGitCredentials    string       `yaml:"injectGitCredentials"`
	InjectDockerCredentials string       `yaml:"injectDockerCredentials"`
	Docker                  DockerConfig `yaml:"docker"`
}

type DockerConfig struct {
	Path    string `yaml:"path"`
	Install bool   `yaml:"install"`
}

type Binaries struct {
	SSHProvider []Binary `yaml:"SSH_PROVIDER"`
}

type Binary struct {
	OS       string `yaml:"os"`
	Arch     string `yaml:"arch"`
	Path     string `yaml:"path"`
	Checksum string `yaml:"checksum"`
}

type buildConfig struct {
	version     string
	projectRoot string
	isRelease   bool
	checksums   map[string]string
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 2 {
		return fmt.Errorf("expected version as argument")
	}

	cfg, err := newBuildConfig(os.Args[1])
	if err != nil {
		return err
	}

	provider, err := buildProvider(cfg)
	if err != nil {
		return err
	}

	output, err := yaml.Marshal(provider)
	if err != nil {
		return fmt.Errorf("marshal yaml: %w", err)
	}

	_, err = os.Stdout.Write(output)
	return err
}

func newBuildConfig(version string) (*buildConfig, error) {
	checksums, err := parseChecksums("./dist/checksums.txt")
	if err != nil {
		return nil, fmt.Errorf("parse checksums: %w", err)
	}

	projectRoot := os.Getenv("PROJECT_ROOT")
	if projectRoot == "" {
		owner := getEnvOrDefault("GITHUB_OWNER", githubOwner)
		projectRoot = fmt.Sprintf("https://github.com/%s/%s/releases/download/%s", owner, githubRepo, version)
	}

	isRelease := strings.Contains(projectRoot, "github.com") && strings.Contains(projectRoot, "/releases/")

	return &buildConfig{
		version:     version,
		projectRoot: projectRoot,
		isRelease:   isRelease,
		checksums:   checksums,
	}, nil
}

func buildProvider(cfg *buildConfig) (Provider, error) {
	binaries, err := buildBinaries(cfg, allPlatforms())
	if err != nil {
		return Provider{}, err
	}
	return Provider{
		Name:         providerName,
		Version:      cfg.version,
		Description:  "DevPod on SSH",
		Icon:         "https://devpod.sh/assets/ssh.svg",
		IconDark:     "https://devpod.sh/assets/ssh_dark.svg",
		OptionGroups: buildOptionGroups(),
		Options:      buildOptions(),
		Agent:        buildAgent(),
		Binaries:     binaries,
		Exec: map[string]string{
			"init":    "${SSH_PROVIDER} init",
			"command": "${SSH_PROVIDER} command",
		},
	}, nil
}

func buildOptionGroups() []OptionGroup {
	return []OptionGroup{
		{
			Name:           "SSH options",
			DefaultVisible: false,
			Options:        []string{"PORT", "EXTRA_FLAGS", "USE_BUILTIN_SSH"},
		},
		{
			Name:           "Agent options",
			DefaultVisible: false,
			Options: []string{
				"DOCKER_PATH", "AGENT_PATH", "INACTIVITY_TIMEOUT",
				"INJECT_DOCKER_CREDENTIALS", "INJECT_GIT_CREDENTIALS",
			},
		},
	}
}

func buildOptions() Options {
	return Options{
		"HOST": {
			Description: "The SSH Host to connect to. Example: my-user@my-domain.com",
			Required:    true,
		},
		"PORT": {
			Description: "The SSH Port to use. Defaults to 22",
			Default:     "22",
		},
		"EXTRA_FLAGS": {
			Description: "Extra flags to pass to the SSH command.",
		},
		"USE_BUILTIN_SSH": {
			Description: "Use the builtin SSH package.",
			Default:     "false",
			Type:        "boolean",
		},
		"DOCKER_PATH": {
			Description: "The path where to find the docker binary.",
			Default:     "docker",
		},
		"AGENT_PATH": {
			Description: "The path where to inject the DevPod agent to.",
			Command:     `printf "%s" "/tmp/${USER}/devpod/agent"`,
		},
		"INACTIVITY_TIMEOUT": {
			Description: "If defined, will automatically stop the container after the inactivity period. Example: 10m",
		},
		"INJECT_GIT_CREDENTIALS": {
			Description: "If DevPod should inject git credentials into the remote host.",
			Default:     "true",
		},
		"INJECT_DOCKER_CREDENTIALS": {
			Description: "If DevPod should inject docker credentials into the remote host.",
			Default:     "true",
		},
	}
}

func buildAgent() Agent {
	return Agent{
		Path:                    "${AGENT_PATH}",
		InactivityTimeout:       "${INACTIVITY_TIMEOUT}",
		InjectGitCredentials:    "${INJECT_GIT_CREDENTIALS}",
		InjectDockerCredentials: "${INJECT_DOCKER_CREDENTIALS}",
		Docker: DockerConfig{
			Path:    "${DOCKER_PATH}",
			Install: false,
		},
	}
}

func buildBinaries(cfg *buildConfig, platforms []string) (Binaries, error) {
	list, err := buildBinaryList(cfg, platforms)
	if err != nil {
		return Binaries{}, err
	}
	return Binaries{SSHProvider: list}, nil
}

func buildBinaryList(cfg *buildConfig, platforms []string) ([]Binary, error) {
	result := make([]Binary, 0, len(platforms))
	for _, platform := range platforms {
		bin, err := buildBinary(cfg, platform)
		if err != nil {
			return nil, err
		}
		result = append(result, bin)
	}
	return result, nil
}

func buildBinary(cfg *buildConfig, platform string) (Binary, error) {
	os, arch, ok := strings.Cut(platform, "/")
	if !ok {
		return Binary{}, fmt.Errorf("invalid platform format: %s (expected os/arch)", platform)
	}

	path, err := buildBinaryPath(cfg, platform)
	if err != nil {
		return Binary{}, err
	}

	filename := buildFilename(os, arch)
	path = joinPath(path, filename)

	return Binary{
		OS:       os,
		Arch:     arch,
		Path:     path,
		Checksum: cfg.checksums[filename],
	}, nil
}

func buildBinaryPath(cfg *buildConfig, platform string) (string, error) {
	path := cfg.projectRoot
	if cfg.isRelease {
		return path, nil
	}

	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return buildURLPath(path, platform)
	}
	return buildFilePath(path, platform)
}

func buildURLPath(base, platform string) (string, error) {
	parsed, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("parse base url: %w", err)
	}
	dir, err := buildDir(platform)
	if err != nil {
		return "", err
	}
	joined, err := url.JoinPath(parsed.String(), dir)
	if err != nil {
		return "", fmt.Errorf("join url path: %w", err)
	}
	return joined, nil
}

func buildFilePath(base, platform string) (string, error) {
	absPath, err := filepath.Abs(base)
	if err != nil {
		return "", fmt.Errorf("get absolute path: %w", err)
	}
	dir, err := buildDir(platform)
	if err != nil {
		return "", err
	}
	return filepath.Join(absPath, dir), nil
}

func buildFilename(os, arch string) string {
	filename := fmt.Sprintf("devpod-provider-%s-%s-%s", providerName, os, arch)
	if os == "windows" {
		filename += ".exe"
	}
	return filename
}

func joinPath(base, filename string) string {
	if strings.HasPrefix(base, "http://") || strings.HasPrefix(base, "https://") {
		joined, _ := url.JoinPath(base, filename)
		return joined
	}
	return filepath.Join(base, filename)
}

func buildDir(platform string) (string, error) {
	dirs := map[string]string{
		"linux/amd64":   "build_linux_amd64_v1",
		"linux/arm64":   "build_linux_arm64_v8.0",
		"darwin/amd64":  "build_darwin_amd64_v1",
		"darwin/arm64":  "build_darwin_arm64_v8.0",
		"windows/amd64": "build_windows_amd64_v1",
	}
	dir, ok := dirs[platform]
	if !ok {
		return "", fmt.Errorf("unsupported platform: %s", platform)
	}
	return dir, nil
}

func allPlatforms() []string {
	return []string{"linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64", "windows/amd64"}
}

func parseChecksums(path string) (map[string]string, error) {
	// #nosec G304 -- path is controlled by build script
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	checksums := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		parts := strings.SplitN(scanner.Text(), "  ", 2)
		if len(parts) == 2 {
			checksums[strings.TrimSpace(parts[1])] = strings.TrimSpace(parts[0])
		} else if checksum, filename, ok := strings.Cut(scanner.Text(), " "); ok {
			checksums[strings.TrimSpace(filename)] = strings.TrimSpace(checksum)
		}
	}

	return checksums, scanner.Err()
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
