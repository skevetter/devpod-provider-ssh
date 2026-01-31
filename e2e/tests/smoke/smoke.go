package smoke

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"

	"github.com/onsi/ginkgo/v2"
	"github.com/skevetter/devpod/e2e/framework"
)

var _ = ginkgo.Describe("[smoke]: devpod provider ssh test suite", ginkgo.Ordered, func() {

	ginkgo.Context("testing /kubeletinfo endpoint", ginkgo.Label("smoke"), ginkgo.Ordered, func() {
		ginkgo.It("should compile the provider", func() {
			// Build using goreleaser
			cmd := exec.Command("goreleaser", "build", "--snapshot", "--clean", "--single-target")
			cmd.Dir = "../"
			err := cmd.Run()
			framework.ExpectNoError(err)

			// Generate provider.yaml
			cmd = exec.Command("go", "run", "./hack/provider/main.go", "0.0.0")
			cmd.Dir = "../"
			output, err := cmd.Output()
			framework.ExpectNoError(err)

			// Write provider.yaml
			err = os.WriteFile("../dist/provider.yaml", output, 0600)
			framework.ExpectNoError(err)

			// Replace binary path in manifest to point to freshly built binaries
			input, err := os.ReadFile("../dist/provider.yaml")
			framework.ExpectNoError(err)

			cwd, err := os.Getwd()
			framework.ExpectNoError(err)

			replaceURL := []byte("https://github.com/skevetter/devpod-provider-ssh/releases/download/0.0.0/")
			replaceWith := []byte(cwd + "/../dist/")
			finalOutput := bytes.ReplaceAll(input, replaceURL, replaceWith)

			err = os.WriteFile("../dist/provider.yaml", finalOutput, 0600)
			framework.ExpectNoError(err)
		})

		ginkgo.It("should generate ssh keypairs", func() {
			_, err := os.Stat(os.Getenv("HOME") + "/.ssh/id_rsa")
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "generating ssh keys\n")
				// #nosec G204 -- HOME is from environment
				cmd := exec.Command("ssh-keygen", "-q", "-t", "rsa", "-N", "", "-f", os.Getenv("HOME")+"/.ssh/id_rsa")
				err = cmd.Run()
				framework.ExpectNoError(err)

				// #nosec G204 -- HOME is from environment
				cmd = exec.Command("ssh-keygen", "-y", "-f", os.Getenv("HOME")+"/.ssh/id_rsa")
				output, err := cmd.Output()
				framework.ExpectNoError(err)

				err = os.WriteFile(os.Getenv("HOME")+"/.ssh/id_rsa.pub", output, 0600)
				framework.ExpectNoError(err)
			}

			// #nosec G204 -- HOME is from environment
			cmd := exec.Command("ssh-keygen", "-y", "-f", os.Getenv("HOME")+"/.ssh/id_rsa")
			publicKey, err := cmd.Output()
			framework.ExpectNoError(err)

			_, err = os.Stat(os.Getenv("HOME") + "/.ssh/authorized_keys")
			if err != nil {
				err = os.WriteFile(os.Getenv("HOME")+"/.ssh/authorized_keys", publicKey, 0600)
				framework.ExpectNoError(err)
			} else {
				f, err := os.OpenFile(os.Getenv("HOME")+"/.ssh/authorized_keys",
					os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
				framework.ExpectNoError(err)

				defer func() { _ = f.Close() }()
				_, err = f.Write(publicKey)
				framework.ExpectNoError(err)
			}
		})

		ginkgo.It("should download latest devpod", func() {
			resp, err := http.Get("https://github.com/skevetter/devpod/releases/latest/download/devpod-linux-amd64")
			framework.ExpectNoError(err)
			defer func() { _ = resp.Body.Close() }()

			err = os.MkdirAll("bin/", 0750)
			framework.ExpectNoError(err)

			out, err := os.Create("bin/devpod")
			framework.ExpectNoError(err)
			defer func() { _ = out.Close() }()

			err = out.Chmod(0755)
			framework.ExpectNoError(err)

			_, err = io.Copy(out, resp.Body)
			framework.ExpectNoError(err)

			err = out.Close()
			framework.ExpectNoError(err)

			// test that devpod works
			cmd := exec.Command("bin/devpod")
			err = cmd.Run()
			framework.ExpectNoError(err)
		})

		ginkgo.It("should add provider to devpod", func() {
			// ensure we don't have the ssh provider present
			cmd := exec.Command("bin/devpod", "provider", "delete", "ssh")
			err := cmd.Run()
			if err != nil {
				_, _ = fmt.Fprintf(os.Stderr, "warning: %v\n", err)
			}

			cmd = exec.Command("bin/devpod", "provider", "add", "../release/provider.yaml", "-o", "HOST=localhost")
			err = cmd.Run()
			framework.ExpectNoError(err)
		})
	})
})
