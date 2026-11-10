package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
)

func TestScanIncludesWorktrees(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	base := t.TempDir()
	repo := filepath.Join(base, "proj")
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	os.MkdirAll(repo, 0o755)
	os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main\n"), 0o644)
	run(repo, "init", "-q", "-b", "main")
	run(repo, "add", ".")
	run(repo, "commit", "-qm", "init")
	// one worktree outside the scanned dir, one tucked in a hidden dir inside it
	run(repo, "worktree", "add", "-q", "-b", "feat/outside", filepath.Join(base, "proj-outside"))
	run(repo, "worktree", "add", "-q", "-b", "hidden", filepath.Join(repo, ".claude", "worktrees", "hidden"))
	os.WriteFile(filepath.Join(base, "proj-outside", "outside.go"), []byte("package main\n"), 0o644)
	os.WriteFile(filepath.Join(repo, ".claude", "worktrees", "hidden", "hidden.go"), []byte("package main\n"), 0o644)

	rels := func(o ScanOpts) []string {
		var out []string
		for _, f := range scan([]string{repo}, o) {
			out = append(out, filepath.ToSlash(f.Rel))
		}
		sort.Strings(out)
		return out
	}
	if got := rels(ScanOpts{}); len(got) != 1 || got[0] != "proj/main.go" {
		t.Fatalf("without the flag, want only proj/main.go, got %v", got)
	}
	got := rels(ScanOpts{Worktrees: true})
	want := []string{
		"proj/main.go",
		"proj@feat-outside/main.go", "proj@feat-outside/outside.go",
		"proj@hidden/hidden.go", "proj@hidden/main.go",
	}
	if len(got) != len(want) {
		t.Fatalf("want %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("want %v, got %v", want, got)
		}
	}
}
