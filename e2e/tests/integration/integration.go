package integration

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/skevetter/devpod/e2e/framework"
)

const providerPath = "../dist/build_linux_amd64_v1/devpod-provider-ssh-linux-amd64"

func setupProvider() {
	cmd := exec.Command("go", "run", "hack/provider/main.go", "0.0.0")
	cmd.Dir = "../"

	projectRoot, err := filepath.Abs("../")
	framework.ExpectNoError(err)

	distPath := filepath.Join(projectRoot, "dist")
	cmd.Env = append(os.Environ(), "PROJECT_ROOT="+distPath)
	output, err := cmd.Output()
	framework.ExpectNoError(err)

	err = os.WriteFile("../dist/provider.yaml", output, 0600)
	framework.ExpectNoError(err)
}

func setupSSHKeys() {
	homeDir := os.Getenv("HOME")
	sshKeyPath := filepath.Join(homeDir, ".ssh", "id_rsa")

	_, err := os.Stat(sshKeyPath) // #nosec G703 -- SSH key path is safely constructed
	if err != nil {
		ginkgo.GinkgoWriter.Println("generating ssh keys")
		// #nosec G204,G702 -- SSH key path is safely constructed
		cmd := exec.Command("ssh-keygen", "-q", "-t", "rsa", "-N", "", "-f", sshKeyPath)
		err = cmd.Run()
		framework.ExpectNoError(err)

		cmd = exec.Command("ssh-keygen", "-y", "-f", sshKeyPath) // #nosec G204,G702
		output, err := cmd.Output()
		framework.ExpectNoError(err)

		pubKeyPath := filepath.Join(homeDir, ".ssh", "id_rsa.pub")
		err = os.WriteFile(pubKeyPath, output, 0600) // #nosec G703
		framework.ExpectNoError(err)
	}

	cmd := exec.Command("ssh-keygen", "-y", "-f", sshKeyPath) // #nosec G204,G702
	publicKey, err := cmd.Output()
	framework.ExpectNoError(err)

	authorizedKeysPath := filepath.Join(homeDir, ".ssh", "authorized_keys")
	_, err = os.Stat(authorizedKeysPath) // #nosec G703 -- authorized_keys path is safely constructed
	if err != nil {
		err = os.WriteFile(authorizedKeysPath, publicKey, 0600) // #nosec G703
		framework.ExpectNoError(err)
	} else {
		// #nosec G304,G703 -- authorized_keys path is safely constructed
		f, err := os.OpenFile(authorizedKeysPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		framework.ExpectNoError(err)

		defer func() { _ = f.Close() }()
		_, err = f.Write(publicKey)
		framework.ExpectNoError(err)
	}
}

func setupDevpodCLI() {
	resp, err := http.Get("https://github.com/skevetter/devpod/releases/latest/download/devpod-linux-amd64")
	framework.ExpectNoError(err)
	defer func() { _ = resp.Body.Close() }()

	binDir := "bin/"
	err = os.MkdirAll(binDir, 0755) // #nosec G301 -- bin directory is safely constructed
	framework.ExpectNoError(err)

	binPath := filepath.Join(binDir, "devpod")
	// #nosec G304,G302 -- path is safely constructed, needs execute permissions
	out, err := os.OpenFile(binPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0755)
	framework.ExpectNoError(err)

	_, err = io.Copy(out, resp.Body)
	framework.ExpectNoError(err)

	err = out.Close()
	framework.ExpectNoError(err)

	ginkgo.GinkgoWriter.Println("File tree:")
	_ = filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err == nil {
			ginkgo.GinkgoWriter.Printf("  %s (mode: %s)\n", path, info.Mode())
		}
		return nil
	})

	// Verify binary is executable
	absPath, _ := filepath.Abs(binPath)
	ginkgo.GinkgoWriter.Printf("Absolute path: %s\n", absPath)

	// #nosec G204 -- executing a known binary for testing purposes
	testCmd := exec.Command(binPath, "version")
	output, err := testCmd.CombinedOutput()
	ginkgo.GinkgoWriter.Printf("Test execution output: %s\n", string(output))
	if err != nil {
		ginkgo.GinkgoWriter.Printf("Test execution error: %v\n", err)
	}
}

var _ = ginkgo.Describe("devpod provider ssh test suite", ginkgo.Label("integration"), ginkgo.Ordered, func() {
	ginkgo.BeforeAll(func() {
		setupProvider()
		setupSSHKeys()
		setupDevpodCLI()
	})

	ginkgo.It("should generate valid provider.yaml", func() {
		data, err := os.ReadFile("../dist/provider.yaml")
		framework.ExpectNoError(err)
		framework.ExpectNotEqual(len(data), 0)
	})

	ginkgo.It("should have required provider options", func() {
		data, err := os.ReadFile("../dist/provider.yaml")
		framework.ExpectNoError(err)

		content := string(data)
		framework.ExpectEqual(strings.Contains(content, "HOST"), true)
		framework.ExpectEqual(strings.Contains(content, "AGENT_PATH"), true)
		framework.ExpectEqual(strings.Contains(content, "DOCKER_PATH"), true)
	})
	ginkgo.It("should install provider with devpod", func() {
		cmd := exec.Command("bin/devpod", "provider", "add", "../dist/provider.yaml", "-o", "HOST=127.0.0.1")
		err := cmd.Run()
		framework.ExpectNoError(err)
	})

	ginkgo.It("should fail the init", func() {
		cmd := exec.Command(providerPath, "init")
		cmd.Env = append(cmd.Environ(), []string{
			"AGENT_PATH=/tmp/devpod/agent",
			"COMMAND=ls",
			"DOCKER_PATH=docker",
			"HOST=localhost",
			"PORT=1234",
			"USE_BUILTIN_SSH=false",
		}...)
		err := cmd.Run()
		framework.ExpectError(err)
	})

	ginkgo.It("should run the init", func() {
		cmd := exec.Command(providerPath, "init")
		cmd.Env = append(cmd.Environ(), []string{
			"AGENT_PATH=/tmp/devpod/agent",
			"COMMAND=ls",
			"DOCKER_PATH=docker",
			"HOST=localhost",
			"PORT=22",
			"USE_BUILTIN_SSH=false",
		}...)
		err := cmd.Run()
		framework.ExpectNoError(err)
	})

	ginkgo.It("should run a command", func() {
		cmd := exec.Command(providerPath, "command")
		cmd.Env = append(cmd.Environ(), []string{
			"AGENT_PATH=/tmp/devpod/agent",
			"COMMAND=ls",
			"DOCKER_PATH=docker",
			"HOST=localhost",
			"PORT=22",
			"USE_BUILTIN_SSH=false",
		}...)
		err := cmd.Run()
		framework.ExpectNoError(err)
	})

	ginkgo.It("should run a command and verify the output", func() {
		cmd := exec.Command("ls", "/")
		controlOutput, err := cmd.Output()
		framework.ExpectNoError(err)

		cmd = exec.Command(providerPath, "command")
		cmd.Env = append(cmd.Environ(), []string{
			"AGENT_PATH=/tmp/devpod/agent",
			"COMMAND=ls /",
			"DOCKER_PATH=docker",
			"HOST=localhost",
			"PORT=22",
			"USE_BUILTIN_SSH=false",
		}...)
		output, err := cmd.Output()
		framework.ExpectNoError(err)

		gomega.Expect(output).To(gomega.Equal(controlOutput))
	})

	ginkgo.It("should run a multiline command and verify the output", func() {
		cmd := exec.Command("echo", `line1
line2
line3`)
		controlOutput, err := cmd.Output()
		framework.ExpectNoError(err)

		_ = os.Setenv("COMMAND", `echo line1
echo line2
echo line3`)

		cmd = exec.Command(providerPath, "command")
		cmd.Env = append(cmd.Environ(), []string{
			"AGENT_PATH=/tmp/devpod/agent",
			"DOCKER_PATH=docker",
			"COMMAND=" + `echo line1
echo line2
echo line3`,
			"HOST=localhost",
			"PORT=22",
			"USE_BUILTIN_SSH=false",
		}...)
		output, err := cmd.CombinedOutput()
		framework.ExpectNoError(err)

		gomega.Expect(output).To(gomega.Equal(controlOutput))
	})

	ginkgo.It("should run a failing command and fail", func() {
		controlOutput := []byte("bash: line 1: not-a-command: command not found")

		cmd := exec.Command(providerPath, "command")
		cmd.Env = append(cmd.Environ(), []string{
			"AGENT_PATH=/tmp/devpod/agent",
			"COMMAND=not-a-command",
			"DOCKER_PATH=docker",
			"HOST=localhost",
			"PORT=22",
			"USE_BUILTIN_SSH=false",
		}...)
		output, err := cmd.CombinedOutput()
		framework.ExpectError(err)

		output = bytes.TrimSpace(output)

		gomega.Expect(output).To(gomega.Equal(controlOutput))
	})

	ginkgo.It("should run devpod up", func() {
		// Debug: Check permissions before first command
		binInfo, _ := os.Stat("bin/devpod")
		ginkgo.GinkgoWriter.Printf("BEFORE up - bin/devpod mode: %s\n", binInfo.Mode())
		binDirInfo, _ := os.Stat("bin")
		ginkgo.GinkgoWriter.Printf("BEFORE up - bin/ mode: %s\n", binDirInfo.Mode())

		cmd := exec.Command("bin/devpod", "up", "--debug", "--ide=none", "../")
		output, err := cmd.CombinedOutput()
		ginkgo.GinkgoWriter.Printf("up output:\n%s\n", string(output))
		framework.ExpectNoError(err)

		// Debug: Check permissions after first command
		binInfo, _ = os.Stat("bin/devpod")
		ginkgo.GinkgoWriter.Printf("AFTER up - bin/devpod mode: %s\n", binInfo.Mode())
		binDirInfo, _ = os.Stat("bin")
		ginkgo.GinkgoWriter.Printf("AFTER up - bin/ mode: %s\n", binDirInfo.Mode())

		cmd = exec.Command("bin/devpod", "list", "--debug", "--output=json")
		output, err = cmd.CombinedOutput()
		ginkgo.GinkgoWriter.Printf("list output:\n%s\n", string(output))
		framework.ExpectNoError(err)

		// Verify workspace is ready
		cmd = exec.Command("bin/devpod",
			"ssh",
			"--debug",
			"--context",
			"default",
			"--user",
			"vscode",
			"devpod-provider-ssh",
			"--command",
			"echo ready",
		)
		output, err = cmd.CombinedOutput()
		ginkgo.GinkgoWriter.Printf("verify output:\n%s\n", string(output))
		if err != nil {
			ginkgo.GinkgoWriter.Printf("Failed: %v\n", err)
		}
		framework.ExpectNoError(err)
	})

	ginkgo.It("should run commands to workspace via ssh", func() {
		cmd := exec.Command("ssh", "devpod-provider-ssh.devpod", "echo", "test")
		output, err := cmd.Output()
		if err != nil {
			ginkgo.GinkgoWriter.Printf("SSH command failed: %v\n", err)
			sshConfigPath := filepath.Join(os.Getenv("HOME"), ".ssh", "config")
			sshConfigData, err := os.ReadFile(sshConfigPath) // #nosec G304 -- SSH config path is safely constructed
			if err != nil {
				ginkgo.GinkgoWriter.Printf("Failed to read SSH config at %s: %v\n", sshConfigPath, err)
			} else {
				ginkgo.GinkgoWriter.Printf("SSH config contents:\n%s\n", string(sshConfigData))
			}
		}
		framework.ExpectNoError(err)

		gomega.Expect(output).To(gomega.Equal([]byte("test\n")))
	})

	ginkgo.It("should cleanup devpod workspace", func() {
		cmd := exec.Command("bin/devpod", "delete", "--debug", "--force", "devpod-provider-ssh")
		err := cmd.Run()
		framework.ExpectNoError(err)
	})
})
