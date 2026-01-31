package ssh

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"
)

type ConfigParserTestSuite struct {
	suite.Suite
	tempDir string
}

func TestConfigParserTestSuite(t *testing.T) {
	suite.Run(t, new(ConfigParserTestSuite))
}

func (s *ConfigParserTestSuite) SetupTest() {
	s.tempDir = s.T().TempDir()
}

func (s *ConfigParserTestSuite) TestParseSSHConfig_NoConfigFile() {
	configPath := filepath.Join(s.tempDir, "nonexistent")
	config, err := ParseSSHConfig("example.com", configPath)

	s.Require().NoError(err)
	s.Equal("example.com", config.Hostname)
	s.Equal("22", config.Port)
	s.NotEmpty(config.User)
	s.NotEmpty(config.IdentityFiles)
}

func (s *ConfigParserTestSuite) TestParseSSHConfig_EmptyConfig() {
	configPath := filepath.Join(s.tempDir, "config")
	err := os.WriteFile(configPath, []byte(""), 0600)
	s.Require().NoError(err)

	config, err := ParseSSHConfig("example.com", configPath)

	s.Require().NoError(err)
	s.Equal("example.com", config.Hostname)
	s.Equal("22", config.Port)
}

func (s *ConfigParserTestSuite) TestParseSSHConfig_SimpleHost() {
	configContent := `Host example
    HostName example.com
    User testuser
    Port 2222
    IdentityFile ~/.ssh/custom_key
`
	configPath := filepath.Join(s.tempDir, "config")
	err := os.WriteFile(configPath, []byte(configContent), 0600)
	s.Require().NoError(err)

	config, err := ParseSSHConfig("example", configPath)

	s.Require().NoError(err)
	s.Equal("example.com", config.Hostname)
	s.Equal("testuser", config.User)
	s.Equal("2222", config.Port)
	// Config starts with defaults, then adds custom key
	s.Greater(len(config.IdentityFiles), 0)
	s.Contains(config.IdentityFiles[len(config.IdentityFiles)-1], ".ssh/custom_key")
}

func (s *ConfigParserTestSuite) TestParseSSHConfig_MultipleHosts() {
	configContent := `Host first
    HostName first.com
    User user1

Host second
    HostName second.com
    User user2
`
	configPath := filepath.Join(s.tempDir, "config")
	err := os.WriteFile(configPath, []byte(configContent), 0600)
	s.Require().NoError(err)

	config, err := ParseSSHConfig("second", configPath)

	s.Require().NoError(err)
	s.Equal("second.com", config.Hostname)
	s.Equal("user2", config.User)
}

func (s *ConfigParserTestSuite) TestParseSSHConfig_WildcardHost() {
	configContent := `Host *
    User defaultuser
    Port 2222
`
	configPath := filepath.Join(s.tempDir, "config")
	err := os.WriteFile(configPath, []byte(configContent), 0600)
	s.Require().NoError(err)

	config, err := ParseSSHConfig("anything.com", configPath)

	s.Require().NoError(err)
	s.Equal("defaultuser", config.User)
	s.Equal("2222", config.Port)
}

func (s *ConfigParserTestSuite) TestMatchHost_ExactMatch() {
	s.True(matchHost("example.com", "example.com"))
}

func (s *ConfigParserTestSuite) TestMatchHost_Wildcard() {
	s.True(matchHost("*", "anything.com"))
}

func (s *ConfigParserTestSuite) TestMatchHost_UserAtHost() {
	s.True(matchHost("example.com", "user@example.com"))
}

func (s *ConfigParserTestSuite) TestMatchHost_NoMatch() {
	s.False(matchHost("example.com", "other.com"))
}

func (s *ConfigParserTestSuite) TestParseConfig_CaseInsensitive() {
	configContent := `Host example
    HOSTNAME example.com
    USER testuser
    PORT 2222
`
	configPath := filepath.Join(s.tempDir, "config")
	err := os.WriteFile(configPath, []byte(configContent), 0600)
	s.Require().NoError(err)

	config, err := ParseSSHConfig("example", configPath)

	s.Require().NoError(err)
	s.Equal("example.com", config.Hostname)
	s.Equal("testuser", config.User)
	s.Equal("2222", config.Port)
}

func (s *ConfigParserTestSuite) TestParseConfig_MultipleIdentityFiles() {
	configContent := `Host example
    IdentityFile ~/.ssh/key1
    IdentityFile ~/.ssh/key2
    IdentityFile ~/.ssh/key3
`
	configPath := filepath.Join(s.tempDir, "config")
	err := os.WriteFile(configPath, []byte(configContent), 0600)
	s.Require().NoError(err)

	config, err := ParseSSHConfig("example", configPath)

	s.Require().NoError(err)
	// Config starts with 3 defaults, adds 3 custom = 6 total
	s.Len(config.IdentityFiles, 6)
	s.Contains(config.IdentityFiles[3], "key1")
	s.Contains(config.IdentityFiles[4], "key2")
	s.Contains(config.IdentityFiles[5], "key3")
}

func (s *ConfigParserTestSuite) TestDefaultConfig_SimpleHost() {
	config := defaultConfig("example.com")

	s.Equal("example.com", config.Hostname)
	s.Equal("22", config.Port)
	s.NotEmpty(config.User)
	s.Len(config.IdentityFiles, 3)
}

func (s *ConfigParserTestSuite) TestDefaultConfig_UserAtHost() {
	config := defaultConfig("myuser@example.com")

	s.Equal("example.com", config.Hostname)
	s.Equal("myuser", config.User)
}

func (s *ConfigParserTestSuite) TestDefaultConfig_DefaultPort() {
	config := defaultConfig("example.com")
	s.Equal("22", config.Port)
}

func (s *ConfigParserTestSuite) TestDefaultConfig_DefaultKeys() {
	config := defaultConfig("example.com")

	s.Len(config.IdentityFiles, 3)
	s.Contains(config.IdentityFiles[0], "id_rsa")
	s.Contains(config.IdentityFiles[1], "id_ecdsa")
	s.Contains(config.IdentityFiles[2], "id_ed25519")
}

func (s *ConfigParserTestSuite) TestExpandPath_TildeExpansion() {
	expanded := expandPath("~/test/path")
	s.NotContains(expanded, "~")
	s.Contains(expanded, "test/path")
}

func (s *ConfigParserTestSuite) TestExpandPath_AbsolutePath() {
	path := "/absolute/path"
	expanded := expandPath(path)
	s.Equal(path, expanded)
}

func (s *ConfigParserTestSuite) TestExpandPath_RelativePath() {
	path := "relative/path"
	expanded := expandPath(path)
	s.Equal(path, expanded)
}

func (s *ConfigParserTestSuite) TestParseConfig_CommentsIgnored() {
	configContent := `# This is a comment
Host example
    # Another comment
    HostName example.com
    User testuser
`
	configPath := filepath.Join(s.tempDir, "config")
	err := os.WriteFile(configPath, []byte(configContent), 0600)
	s.Require().NoError(err)

	config, err := ParseSSHConfig("example", configPath)

	s.Require().NoError(err)
	s.Equal("example.com", config.Hostname)
	s.Equal("testuser", config.User)
}

func (s *ConfigParserTestSuite) TestParseConfig_EmptyLines() {
	configContent := `
Host example

    HostName example.com

    User testuser

`
	configPath := filepath.Join(s.tempDir, "config")
	err := os.WriteFile(configPath, []byte(configContent), 0600)
	s.Require().NoError(err)

	config, err := ParseSSHConfig("example", configPath)

	s.Require().NoError(err)
	s.Equal("example.com", config.Hostname)
	s.Equal("testuser", config.User)
}

func (s *ConfigParserTestSuite) TestParseConfig_MalformedLines() {
	configContent := `Host example
    HostName example.com
    InvalidLine
    User testuser
    AnotherInvalidLine WithoutValue
`
	configPath := filepath.Join(s.tempDir, "config")
	err := os.WriteFile(configPath, []byte(configContent), 0600)
	s.Require().NoError(err)

	config, err := ParseSSHConfig("example", configPath)

	s.Require().NoError(err)
	s.Equal("example.com", config.Hostname)
	s.Equal("testuser", config.User)
}
