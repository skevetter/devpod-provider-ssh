package framework

import (
	"runtime"
)

type Framework struct {
	DevpodBinDir  string
	DevpodBinName string
}

func NewDefaultFramework(path string) *Framework {
	var binName = "devpod-"
	switch runtime.GOOS {
	case "darwin":
		binName += "darwin-"
	case "linux":
		binName += "linux-"
	}

	switch runtime.GOARCH {
	case "amd64":
		binName += "amd64"
	case "arm64":
		binName += "arm64"
	}
	return &Framework{DevpodBinDir: path, DevpodBinName: binName}
}
