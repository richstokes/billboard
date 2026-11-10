package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRepoURLs(t *testing.T) {
	if got := normalizeRepoURL("richstokes/billboard"); got != "https://github.com/richstokes/billboard.git" {
		t.Errorf("shorthand: got %s", got)
	}
	if got := normalizeRepoURL("git@gitlab.com:a/b.git"); got != "git@gitlab.com:a/b.git" {
		t.Errorf("scp url should be untouched: got %s", got)
	}
	base := os.TempDir()
	want := filepath.Join(base, "billboard", "repos", "github.com", "owner", "name")
	for _, u := range []string{
		"https://github.com/owner/name.git",
		"https://github.com/owner/name",
		"git@github.com:owner/name.git",
		"https://user:s3cret@github.com/owner/name.git",
		"ssh://git@github.com/owner/name.git",
	} {
		got, err := repoCacheDir(u)
		if err != nil || got != want {
			t.Errorf("%s: got %s, want %s", u, got, want)
		}
	}
}
