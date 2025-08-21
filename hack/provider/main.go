package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	urlpkg "net/url"
	"os"
	"strings"

	"github.com/go-git/go-git/v6"
)

var checksumMap = map[string]string{
	"./release/devpod-provider-ssh-linux-amd64":       "##CHECKSUM_LINUX_AMD64##",
	"./release/devpod-provider-ssh-linux-arm64":       "##CHECKSUM_LINUX_ARM64##",
	"./release/devpod-provider-ssh-darwin-amd64":      "##CHECKSUM_DARWIN_AMD64##",
	"./release/devpod-provider-ssh-darwin-arm64":      "##CHECKSUM_DARWIN_ARM64##",
	"./release/devpod-provider-ssh-windows-amd64.exe": "##CHECKSUM_WINDOWS_AMD64##",
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "Expected version as argument")
		os.Exit(1)
		return
	}

	content, err := os.ReadFile("./hack/provider/provider.yaml")
	if err != nil {
		panic(err)
	}

	replaced := strings.ReplaceAll(string(content), "##VERSION##", os.Args[1])
	for k, v := range checksumMap {
		checksum, err := File(k)
		if err != nil {
			panic(fmt.Errorf("generate checksum for %s: %w", k, err))
		}

		replaced = strings.ReplaceAll(replaced, v, checksum)
	}

	repo, err := git.PlainOpen(".")
	if err != nil {
		panic(fmt.Errorf("failed to open git repository: %w", err))
	}

	owner, project, err := ownerRepoFromRemotes(repo)
	if err != nil {
		panic(fmt.Errorf("failed to resolve owner/repo from git remotes: %w", err))
	}
	replaced = strings.ReplaceAll(replaced, "##GIT_USER##", owner)
	replaced = strings.ReplaceAll(replaced, "##GIT_REPO##", project)

	fmt.Print(replaced)
}

func ownerRepoFromRemotes(r *git.Repository) (string, string, error) {
	if remote, err := r.Remote("origin"); err == nil {
		if owner, repo, ok := parseOwnerRepoFromURLs(remote.Config().URLs); ok {
			return owner, repo, nil
		}
	}

	remotes, err := r.Remotes()
	if err != nil {
		return "", "", err
	}
	for _, remote := range remotes {
		if owner, repo, ok := parseOwnerRepoFromURLs(remote.Config().URLs); ok {
			return owner, repo, nil
		}
	}
	return "", "", fmt.Errorf("no suitable remote URLs found")
}

func parseOwnerRepoFromURLs(urls []string) (string, string, bool) {
	for _, u := range urls {
		owner, repo, ok := parseOwnerRepo(u)
		if ok {
			return owner, repo, true
		}
	}
	return "", "", false
}

// parseOwnerRepo parses common git remote URL formats to extract owner and repo.
// Supports:
// - git@host:owner/repo.git
// - ssh://git@host/owner/repo.git
// - https://host/owner/repo.git
// - https://host/owner/repo
func parseOwnerRepo(remoteURL string) (string, string, bool) {
	var path string

	if strings.Contains(remoteURL, ":") && !strings.HasPrefix(remoteURL, "http://") && !strings.HasPrefix(remoteURL, "https://") && !strings.HasPrefix(remoteURL, "ssh://") {
		parts := strings.SplitN(remoteURL, ":", 2)
		if len(parts) == 2 {
			path = parts[1]
		}
	} else {
		if u, err := urlpkg.Parse(remoteURL); err == nil {
			path = strings.TrimPrefix(u.Path, "/")
		}
	}

	if path == "" {
		return "", "", false
	}
	path = strings.TrimSuffix(path, ".git")
	segs := strings.Split(path, "/")
	if len(segs) < 2 {
		return "", "", false
	}
	owner := segs[len(segs)-2]
	repo := segs[len(segs)-1]
	return owner, repo, true
}

// File hashes a given file to a sha256 string
func File(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	_, err = io.Copy(hash, file)
	if err != nil {
		return "", err
	}

	return strings.ToLower(hex.EncodeToString(hash.Sum(nil))), nil
}
