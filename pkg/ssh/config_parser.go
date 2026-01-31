package ssh

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// SSHConfig represents parsed SSH configuration.
type SSHConfig struct {
	Hostname      string
	User          string
	Port          string
	IdentityFiles []string
}

// ParseSSHConfig parses SSH config file for a given host.
func ParseSSHConfig(host, configPath string) (*SSHConfig, error) {
	if configPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		configPath = filepath.Join(home, ".ssh", "config")
	}

	// #nosec G304 -- configPath is from user's SSH config location
	file, err := os.Open(configPath)
	if err != nil {
		// If config does not exist, return defaults
		if os.IsNotExist(err) {
			return defaultConfig(host), nil
		}
		return nil, err
	}
	defer func() { _ = file.Close() }()

	return parseSSHConfigFile(file, host)
}

func parseSSHConfigFile(file *os.File, host string) (*SSHConfig, error) {
	config := defaultConfig(host)
	inMatchingHost := false
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip comments and empty lines
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		key := strings.ToLower(fields[0])
		value := fields[1]

		// Check for Host directive
		if key == "host" {
			inMatchingHost = matchHost(value, host)
			continue
		}

		// Only parse if we're in a matching host block
		if !inMatchingHost {
			continue
		}

		applyConfigDirective(config, key, value)
	}

	return config, scanner.Err()
}

func applyConfigDirective(config *SSHConfig, key, value string) {
	switch key {
	case "hostname":
		config.Hostname = value
	case "user":
		config.User = value
	case "port":
		config.Port = value
	case "identityfile":
		expanded := expandPath(value)
		config.IdentityFiles = append(config.IdentityFiles, expanded)
	}
}

func defaultConfig(host string) *SSHConfig {
	// Extract user@host if present
	user := ""
	hostname := host
	if strings.Contains(host, "@") {
		parts := strings.SplitN(host, "@", 2)
		user = parts[0]
		hostname = parts[1]
	}

	// Get default user if not specified
	if user == "" {
		user = os.Getenv("USER")
		if user == "" {
			user = "root"
		}
	}

	home, _ := os.UserHomeDir()
	defaultKeys := []string{
		filepath.Join(home, ".ssh", "id_rsa"),
		filepath.Join(home, ".ssh", "id_ecdsa"),
		filepath.Join(home, ".ssh", "id_ed25519"),
	}

	return &SSHConfig{
		Hostname:      hostname,
		User:          user,
		Port:          "22",
		IdentityFiles: defaultKeys,
	}
}

func matchHost(pattern, host string) bool {
	// Simple wildcard matching
	if pattern == "*" {
		return true
	}
	if pattern == host {
		return true
	}
	// Strip user@ prefix for matching
	if strings.Contains(host, "@") {
		host = strings.SplitN(host, "@", 2)[1]
	}
	return pattern == host
}

func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}
