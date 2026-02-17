package ssh

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/suite"
)

type ClientInterfaceTestSuite struct {
	suite.Suite
}

func TestClientInterfaceTestSuite(t *testing.T) {
	suite.Run(t, new(ClientInterfaceTestSuite))
}

func (s *ClientInterfaceTestSuite) TestUnsupportedConfigError_Error() {
	err := &UnsupportedConfigError{Directive: "ProxyJump"}
	s.Equal("unsupported SSH config directive: ProxyJump", err.Error())
}

func (s *ClientInterfaceTestSuite) TestAuthenticationMethodError_Error() {
	err := &AuthenticationMethodError{Method: "GSSAPI"}
	s.Equal("unsupported authentication method: GSSAPI", err.Error())
}

func (s *ClientInterfaceTestSuite) TestKeyFormatError_Error() {
	err := &KeyFormatError{Format: "no valid keys found"}
	s.Equal("unsupported key format: no valid keys found", err.Error())
}

func (s *ClientInterfaceTestSuite) TestShouldFallback_UnsupportedConfig() {
	err := &UnsupportedConfigError{Directive: "ProxyJump"}
	s.True(shouldFallback(err))
}

func (s *ClientInterfaceTestSuite) TestShouldFallback_AuthMethod() {
	err := &AuthenticationMethodError{Method: "GSSAPI"}
	s.True(shouldFallback(err))
}

func (s *ClientInterfaceTestSuite) TestShouldFallback_KeyFormat() {
	err := &KeyFormatError{Format: "invalid"}
	s.True(shouldFallback(err))
}

func (s *ClientInterfaceTestSuite) TestShouldFallback_OtherError() {
	err := fmt.Errorf("some other error")
	s.False(shouldFallback(err))
}

func (s *ClientInterfaceTestSuite) TestShouldFallback_Nil() {
	s.False(shouldFallback(nil))
}
