package util

import (
	"fmt"
	"os"
	"strings"
)

func ResolveHomeDirToAbs(path string) (string, error) {
	if strings.HasPrefix(path, "~") {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get user home directory: %w", err)
		}
		return strings.Replace(path, "~", homeDir, 1), nil
	}
	return path, nil
}

func TrimWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
