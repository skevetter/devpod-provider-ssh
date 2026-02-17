package ssh

import (
	"testing"

	"github.com/skevetter/devpod-provider-ssh/pkg/options"
	"github.com/skevetter/log"
	"github.com/stretchr/testify/suite"
)

type ShellSSHClientTestSuite struct {
	suite.Suite
	client *ShellSSHClient
	config *options.Options
}

func TestShellSSHClientTestSuite(t *testing.T) {
	suite.Run(t, new(ShellSSHClientTestSuite))
}

func (s *ShellSSHClientTestSuite) SetupTest() {
	s.config = &options.Options{
		Host:       "testuser@example.com",
		Port:       "22",
		ExtraFlags: "",
	}
	s.client = NewShellSSHClient(s.config, log.Default)
}

func (s *ShellSSHClientTestSuite) TestConnect_NoOp() {
	err := s.client.Connect()
	s.NoError(err)
}

func (s *ShellSSHClientTestSuite) TestClose_NoOp() {
	err := s.client.Close()
	s.NoError(err)
}

func (s *ShellSSHClientTestSuite) TestGetSSHCommand_DefaultPort() {
	cmd, err := s.client.getSSHCommand()

	s.Require().NoError(err)
	s.Contains(cmd, "-oStrictHostKeyChecking=no")
	s.Contains(cmd, "-oBatchMode=yes")
	s.Contains(cmd, "testuser@example.com")
	s.NotContains(cmd, "-p")
}

func (s *ShellSSHClientTestSuite) TestGetSSHCommand_CustomPort() {
	s.client.config.Port = "2222"

	cmd, err := s.client.getSSHCommand()

	s.Require().NoError(err)
	s.Contains(cmd, "-p")
	s.Contains(cmd, "2222")
}

func (s *ShellSSHClientTestSuite) TestGetSSHCommand_ExtraFlags() {
	s.client.config.ExtraFlags = "-v -o ConnectTimeout=10"

	cmd, err := s.client.getSSHCommand()

	s.Require().NoError(err)
	s.Contains(cmd, "-v")
	s.Contains(cmd, "-o")
	s.Contains(cmd, "ConnectTimeout=10")
}

func (s *ShellSSHClientTestSuite) TestGetSSHCommand_ParseError() {
	s.client.config.ExtraFlags = "invalid'quote"

	_, err := s.client.getSSHCommand()

	s.Error(err)
	s.Contains(err.Error(), "parse extra flags")
}

func (s *ShellSSHClientTestSuite) TestGetSCPCommand_DefaultPort() {
	cmd, err := s.client.getSCPCommand("/local/file", "/remote/file")

	s.Require().NoError(err)
	s.Contains(cmd, "-oStrictHostKeyChecking=no")
	s.Contains(cmd, "-oBatchMode=yes")
	s.Contains(cmd, "/local/file")
	s.Contains(cmd, "testuser@example.com:/remote/file")
	s.NotContains(cmd, "-P")
}

func (s *ShellSSHClientTestSuite) TestGetSCPCommand_CustomPort() {
	s.client.config.Port = "2222"

	cmd, err := s.client.getSCPCommand("/local/file", "/remote/file")

	s.Require().NoError(err)
	s.Contains(cmd, "-P")
	s.Contains(cmd, "2222")
}

func (s *ShellSSHClientTestSuite) TestGetSCPCommand_ExtraFlags() {
	s.client.config.ExtraFlags = "-v"

	cmd, err := s.client.getSCPCommand("/local/file", "/remote/file")

	s.Require().NoError(err)
	s.Contains(cmd, "-v")
}

func (s *ShellSSHClientTestSuite) TestGetSCPCommand_DestinationFormat() {
	cmd, err := s.client.getSCPCommand("/local/file", "/remote/file")

	s.Require().NoError(err)
	// Verify destination format is correct
	s.Contains(cmd, "testuser@example.com:/remote/file")
}

func (s *ShellSSHClientTestSuite) TestExecute_StderrCapture() {
	// This test would require mocking exec.Command
	// For now, we'll test the command building logic
	s.T().Skip("Requires exec.Command mocking")
}

func (s *ShellSSHClientTestSuite) TestUpload_CommandBuilding() {
	// Test that upload builds correct SCP command
	// Actual execution would require mocking
	cmd, err := s.client.getSCPCommand("/tmp/test", "/remote/test")

	s.Require().NoError(err)
	s.NotEmpty(cmd)
}
