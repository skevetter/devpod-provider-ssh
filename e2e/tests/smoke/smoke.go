package smoke

import (
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/onsi/ginkgo/v2"
	"github.com/skevetter/devpod/e2e/framework"
)

var _ = ginkgo.Describe("[smoke] devpod provider ssh test suite", ginkgo.Label("smoke"), ginkgo.Ordered, func() {
	ginkgo.BeforeAll(func() {
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
		homeDir := os.Getenv("HOME")
		sshKeyPath := filepath.Join(homeDir, ".ssh", "id_rsa")

		_, err := os.Stat(sshKeyPath)
		if err != nil {
			ginkgo.GinkgoWriter.Println("generating ssh keys")
			cmd := exec.Command("ssh-keygen", "-q", "-t", "rsa", "-N", "", "-f", sshKeyPath) // #nosec G204
			err = cmd.Run()
			framework.ExpectNoError(err)

			cmd = exec.Command("ssh-keygen", "-y", "-f", sshKeyPath) // #nosec G204
			output, err := cmd.Output()
			framework.ExpectNoError(err)

			err = os.WriteFile(filepath.Join(homeDir, ".ssh", "id_rsa.pub"), output, 0600)
			framework.ExpectNoError(err)
		}

		cmd := exec.Command("ssh-keygen", "-y", "-f", sshKeyPath) // #nosec G204
		publicKey, err := cmd.Output()
		framework.ExpectNoError(err)

		authorizedKeysPath := filepath.Join(homeDir, ".ssh", "authorized_keys")
		_, err = os.Stat(authorizedKeysPath)
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

		resp, err := http.Get("https://github.com/skevetter/devpod/releases/latest/download/devpod-linux-amd64")
		framework.ExpectNoError(err)
		defer func() { _ = resp.Body.Close() }()

		err = os.MkdirAll("bin/", 0750)
		framework.ExpectNoError(err)

		out, err := os.Create("bin/devpod")
		framework.ExpectNoError(err)

		_, err = io.Copy(out, resp.Body)
		framework.ExpectNoError(err)

		err = out.Close()
		framework.ExpectNoError(err)

		err = os.Chmod("bin/devpod", 0755) // #nosec G302
		framework.ExpectNoError(err)

		cmd = exec.Command("bin/devpod", "provider", "add", "../dist/provider.yaml", "-o", "HOST=127.0.0.1")
		err = cmd.Run()
		framework.ExpectNoError(err)
	})
})
