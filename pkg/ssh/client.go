package ssh

import (
	"errors"
	"fmt"
	"io"
)

// SSHClient defines the interface for SSH operations.
type SSHClient interface {
	// Connect establishes the SSH connection
	Connect() error

	// Execute runs a command on the remote host
	Execute(command string, output io.Writer) error

	// Upload transfers a file to the remote host
	Upload(localPath, remotePath string) error

	// Close terminates the SSH connection
	Close() error
}

// Error types for fallback detection

// UnsupportedConfigError indicates an SSH config directive is not supported.
type UnsupportedConfigError struct {
	Directive string
}

func (e *UnsupportedConfigError) Error() string {
	return fmt.Sprintf("unsupported SSH config directive: %s", e.Directive)
}

// AuthenticationMethodError indicates an authentication method is not supported.
type AuthenticationMethodError struct {
	Method string
}

func (e *AuthenticationMethodError) Error() string {
	return fmt.Sprintf("unsupported authentication method: %s", e.Method)
}

// KeyFormatError indicates a private key format cannot be parsed.
type KeyFormatError struct {
	Format string
}

func (e *KeyFormatError) Error() string {
	return fmt.Sprintf("unsupported key format: %s", e.Format)
}

// shouldFallback determines if an error should trigger fallback to shell SSH.
func shouldFallback(err error) bool {
	if err == nil {
		return false
	}

	var unsupported *UnsupportedConfigError
	var authMethod *AuthenticationMethodError
	var keyFormat *KeyFormatError

	if errors.As(err, &unsupported) || errors.As(err, &authMethod) || errors.As(err, &keyFormat) {
		return true
	}

	return false
}
