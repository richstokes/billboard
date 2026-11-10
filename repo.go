package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// --repo clones a remote into a cache dir we own, so it's safe to hard-reset it to
// whatever the remote has. A shallow clone keeps it quick on big repos while still
// leaving enough history for the diff effect.
const repoDepth = "200"

var ownerName = regexp.MustCompile(`^[\w.-]+/[\w.-]+$`)

// normalizeRepoURL turns "owner/name" into a GitHub URL and leaves real URLs alone.
func normalizeRepoURL(s string) string {
	if ownerName.MatchString(s) && !strings.HasPrefix(s, ".") {
		return "https://github.com/" + strings.TrimSuffix(s, ".git") + ".git"
	}
	return s
}

// repoCacheDir maps a URL to a stable dir, e.g. ~/Library/Caches/billboard/repos/github.com/owner/name.
func repoCacheDir(url string) (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	u := url
	if i := strings.Index(u, "://"); i >= 0 {
		u = u[i+3:]
	}
	slash := strings.Index(u+"/", "/")
	if at := strings.LastIndex(u[:slash], "@"); at >= 0 { // drop git@ / user:token@
		u = u[at+1:]
	}
	u = strings.TrimSuffix(strings.ReplaceAll(u, ":", "/"), ".git")
	var parts []string
	for _, p := range strings.Split(u, "/") {
		if p != "" && p != "." && p != ".." {
			parts = append(parts, p)
		}
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("can't make sense of repo %q", url)
	}
	return filepath.Join(append([]string{base, "billboard", "repos"}, parts...)...), nil
}

// gitRun runs git with a timeout. Interactive runs (before the UI starts) can show
// progress and prompt for credentials; background ones must never block on a prompt.
func gitRun(dir string, interactive bool, timeout time.Duration, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	if interactive {
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stderr, os.Stderr
	} else {
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_SSH_COMMAND=ssh -o BatchMode=yes")
	}
	return cmd.Run()
}

// syncRepo makes sure there's an up-to-date clone of url and returns its path.
// An existing clone that can't be updated (offline, say) is still used.
func syncRepo(url string) (string, error) {
	dir, err := repoCacheDir(url)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		fmt.Fprintf(os.Stderr, "billboard: updating %s\n", url)
		if err := pullRepo(dir, true); err != nil {
			fmt.Fprintf(os.Stderr, "billboard: couldn't update (%v), using the copy from last time\n", err)
		}
		return dir, nil
	}
	fmt.Fprintf(os.Stderr, "billboard: cloning %s\n", url)
	os.MkdirAll(filepath.Dir(dir), 0o755)
	tmp := dir + ".partial"
	os.RemoveAll(tmp)
	if err := gitRun("", true, 30*time.Minute, "clone", "--depth", repoDepth, "--no-tags", url, tmp); err != nil {
		os.RemoveAll(tmp)
		return "", fmt.Errorf("clone failed: %v", err)
	}
	return dir, os.Rename(tmp, dir)
}

// pullRepo fetches the checked-out branch and hard-resets to it. git only rewrites
// files that changed, so the --watch poller sees exactly what the new commits touched.
func pullRepo(dir string, interactive bool) error {
	branch, err := git(dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return err
	}
	branch = strings.TrimSpace(branch)
	if err := gitRun(dir, interactive, 5*time.Minute, "fetch", "-q", "--depth", repoDepth, "--no-tags", "origin", branch); err != nil {
		return err
	}
	return gitRun(dir, false, time.Minute, "reset", "-q", "--hard", "FETCH_HEAD")
}

// pollRepo keeps pulling in the background for --watch. Failures (network blips,
// rate limits) are ignored and retried next time round.
func pollRepo(dir string, every time.Duration) {
	for range time.Tick(every) {
		pullRepo(dir, false)
	}
}
