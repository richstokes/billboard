package main

import (
	"context"
	"math/rand"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type DiffRow struct {
	G    Line
	Kind byte // ' ' context, '-' removed, '+' added
	No   int  // old line number for '-', new otherwise
}

type CommitDiff struct {
	Hash, Subject, When string
	Hunks               [][]DiffRow
}

var repoRoots = map[string]string{} // dir -> repo root, "" if not in a repo

func git(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir, "-c", "core.quotepath=off"}, args...)...)
	out, err := cmd.Output()
	return string(out), err
}

func repoRoot(dir string) string {
	if r, ok := repoRoots[dir]; ok {
		return r
	}
	out, err := git(dir, "rev-parse", "--show-toplevel")
	root := ""
	if err == nil {
		root = strings.TrimSpace(out)
	}
	repoRoots[dir] = root
	return root
}

// loadDiff picks a random commit that touched f and returns up to 3 of its hunks,
// each trimmed to maxRows.
func loadDiff(f SourceFile, maxRows int, rng *rand.Rand) (*CommitDiff, bool) {
	root := repoRoot(filepath.Dir(f.Abs))
	if root == "" {
		return nil, false
	}
	rel, err := filepath.Rel(root, f.Abs)
	if err != nil {
		return nil, false
	}
	rel = filepath.ToSlash(rel)
	log, err := git(root, "log", "--no-merges", "-n", "40", "--format=%h%x1f%s%x1f%ar", "--", rel)
	if err != nil {
		return nil, false
	}
	commits := strings.Split(strings.TrimSpace(log), "\n")
	// the oldest commit is usually the file being created, which is just one long
	// add; only fall back to it when there's nothing else
	first := commits[len(commits)-1]
	if len(commits) > 1 {
		commits = commits[:len(commits)-1]
	}
	rng.Shuffle(len(commits), func(i, j int) { commits[i], commits[j] = commits[j], commits[i] })
	commits = append(commits[:min(4, len(commits))], first)
	for _, c := range commits {
		p := strings.Split(c, "\x1f")
		if len(p) != 3 {
			continue
		}
		cd := &CommitDiff{Hash: p[0], Subject: p[1], When: p[2]}
		newSrc, err := git(root, "show", cd.Hash+":"+rel)
		if err != nil {
			continue
		}
		oldSrc, _ := git(root, "show", cd.Hash+"^:"+rel) // fails for new files, that's fine
		diff, err := git(root, "show", "--format=", "--no-color", "--no-ext-diff", "-U3", cd.Hash, "--", rel)
		// old versions can hold secrets that were later removed, so check everything
		if err != nil || hasSecret(newSrc) || hasSecret(oldSrc) || hasSecret(diff) {
			continue
		}
		cd.Hunks = parseHunks(diff, highlight(rel, oldSrc), highlight(rel, newSrc), maxRows)
		if len(cd.Hunks) == 0 {
			continue
		}
		if len(cd.Hunks) > 3 {
			i := rng.Intn(len(cd.Hunks) - 2)
			cd.Hunks = cd.Hunks[i : i+3]
		}
		return cd, true
	}
	return nil, false
}

func parseHunks(diff string, oldL, newL []Line, maxRows int) [][]DiffRow {
	get := func(ls []Line, n int) Line {
		if n >= 1 && n <= len(ls) {
			return ls[n-1]
		}
		return nil
	}
	var hunks [][]DiffRow
	var cur []DiffRow
	o, n := 0, 0
	flush := func() {
		changed := false
		for _, r := range cur {
			changed = changed || r.Kind != ' '
		}
		if changed {
			hunks = append(hunks, cur[:min(len(cur), maxRows)])
		}
		cur = nil
	}
	inHunk := false
	for _, ln := range strings.Split(diff, "\n") {
		if strings.HasPrefix(ln, "@@") {
			flush()
			o, n = hunkStart(ln, '-'), hunkStart(ln, '+')
			inHunk = true
			continue
		}
		if !inHunk || ln == "" {
			continue
		}
		switch ln[0] {
		case ' ':
			cur = append(cur, DiffRow{get(newL, n), ' ', n})
			o++
			n++
		case '-':
			cur = append(cur, DiffRow{get(oldL, o), '-', o})
			o++
		case '+':
			cur = append(cur, DiffRow{get(newL, n), '+', n})
			n++
		}
	}
	flush()
	return hunks
}

// hunkStart pulls the start line for side ('-' or '+') out of "@@ -12,7 +12,9 @@".
func hunkStart(h string, side byte) int {
	i := strings.IndexByte(h, side)
	if i < 0 {
		return 1
	}
	s := h[i+1:]
	end := strings.IndexAny(s, ", ")
	if end < 0 {
		end = len(s)
	}
	v, _ := strconv.Atoi(s[:end])
	return max(v, 1)
}
