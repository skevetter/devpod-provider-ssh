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
		cmd := exec.Command("ssh-keygen", "-q", "-t", "rsa", "-N", "", "-f", sshKeyPath) // #nosec G702 -- SSH key path is safely constructed
		err = cmd.Run()
		framework.ExpectNoError(err)

		cmd = exec.Command("ssh-keygen", "-y", "-f", sshKeyPath) // #nosec G702 -- SSH key path is safely constructed
		output, err := cmd.Output()
		framework.ExpectNoError(err)

		err = os.WriteFile(filepath.Join(homeDir, ".ssh", "id_rsa.pub"), output, 0600) // #nosec G703 -- SSH public key path is safely constructed
		framework.ExpectNoError(err)
	}

	cmd := exec.Command("ssh-keygen", "-y", "-f", sshKeyPath) // #nosec G702 -- SSH key path is safely constructed
	publicKey, err := cmd.Output()
	framework.ExpectNoError(err)

	authorizedKeysPath := filepath.Join(homeDir, ".ssh", "authorized_keys")
	_, err = os.Stat(authorizedKeysPath) // #nosec G703 -- authorized_keys path is safely constructed
	if err != nil {
		err = os.WriteFile(authorizedKeysPath, publicKey, 0600)
		framework.ExpectNoError(err)
	} else {
		f, err := os.OpenFile(authorizedKeysPath, // #nosec G304 -- authorized_keys path is safely constructed
			os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
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

	err = os.MkdirAll("bin/", 0750)
	framework.ExpectNoError(err)

	absPath, err := filepath.Abs("bin/devpod")
	framework.ExpectNoError(err)

	out, err := os.Create(absPath)
	framework.ExpectNoError(err)

	_, err = io.Copy(out, resp.Body)
	framework.ExpectNoError(err)

	err = out.Close()
	framework.ExpectNoError(err)

	err = os.Chmod(absPath, 0755) // #nosec G302 -- devpod CLI needs execute permissions
	framework.ExpectNoError(err)
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
		cmd := exec.Command("bin/devpod", "up", "--debug", "--ide=none", "../")
		err := cmd.Run()
		framework.ExpectNoError(err)
	})

	ginkgo.It("should run commands to workspace via ssh", func() {
		cmd := exec.Command("ssh", "devpod-provider-ssh.devpod", "echo", "test")
		output, err := cmd.Output()
		framework.ExpectNoError(err)

		gomega.Expect(output).To(gomega.Equal([]byte("test\n")))
	})

	ginkgo.It("should cleanup devpod workspace", func() {
		cmd := exec.Command("bin/devpod", "delete", "--debug", "--force", "devpod-provider-ssh")
		err := cmd.Run()
		framework.ExpectNoError(err)
	})
})
