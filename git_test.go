package main

import (
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// git reports the repo root with symlinks resolved, which used to break the path
// maths for anything reached through a symlink ($TMPDIR on macOS, so every --repo clone).
func TestLoadDiffThroughSymlink(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	base := t.TempDir()
	repo := filepath.Join(base, "real")
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	os.MkdirAll(filepath.Join(repo, "pkg"), 0o755)
	file := filepath.Join(repo, "pkg", "a.go")
	os.WriteFile(file, []byte("package pkg\n\nfunc A() int { return 1 }\n"), 0o644)
	run("init", "-q", "-b", "main")
	run("add", ".")
	run("commit", "-qm", "add a")
	os.WriteFile(file, []byte("package pkg\n\nfunc A() int { return 2 }\n"), 0o644)
	run("commit", "-qam", "change a")

	link := filepath.Join(base, "link")
	if err := os.Symlink(repo, link); err != nil {
		t.Skip("can't symlink here")
	}
	f := SourceFile{Abs: filepath.Join(link, "pkg", "a.go"), Rel: "link/pkg/a.go", Ext: ".go"}
	cd, ok := loadDiff(f, 20, rand.New(rand.NewSource(1)), true)
	if !ok {
		t.Fatal("no diff found for a file reached through a symlink")
	}
	if cd.Subject != "change a" || len(cd.Hunks) != 1 {
		t.Errorf("want the newest commit with one hunk, got %q with %d", cd.Subject, len(cd.Hunks))
	}
}

func TestParseHunksDropsTrimmedOutChanges(t *testing.T) {
	diff := "@@ -1,4 +1,4 @@\n a\n b\n c\n-d\n+D\n"
	if h := parseHunks(diff, nil, nil, 2); len(h) != 0 {
		t.Errorf("a hunk whose changes got trimmed off shouldn't be kept, got %d", len(h))
	}
	if h := parseHunks(diff, nil, nil, 10); len(h) != 1 || len(h[0]) != 5 {
		t.Errorf("want one hunk of 5 rows, got %v", h)
	}
}
