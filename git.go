package main

import (
	"context"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
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

// Both caches are hit from the --watch goroutine as well as the main loop.
var (
	gitMu     sync.Mutex
	repoRoots = map[string]repoLoc{} // dir -> where it sits in a repo
	wtCache   = map[string]wtEntry{}
)

// repoLoc is a dir's repo root ("" if it isn't in one) and the dir's path inside
// that repo, slash-separated with a trailing slash ("" at the root).
type repoLoc struct {
	root, prefix string
}

type wtEntry struct {
	at  time.Time
	wts []Worktree
}

type Worktree struct {
	Path, Label string
}

func git(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir, "-c", "core.quotepath=off"}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
	cmd.WaitDelay = time.Second // don't hang on a child that outlives the timeout
	out, err := cmd.Output()
	return string(out), err
}

func repoRoot(dir string) string { return repoLocOf(dir).root }

// repoLocOf asks git where dir is rather than working it out from the paths. git
// reports the root with symlinks resolved (/private/var/... for $TMPDIR on macOS),
// so filepath.Rel against the path we scanned can come out as nonsense.
func repoLocOf(dir string) repoLoc {
	gitMu.Lock()
	r, ok := repoRoots[dir]
	gitMu.Unlock()
	if ok {
		return r
	}
	var loc repoLoc
	if out, err := git(dir, "rev-parse", "--show-toplevel", "--show-prefix"); err == nil {
		if p := strings.SplitN(strings.TrimRight(out, "\r\n"), "\n", 2); p[0] != "" {
			loc.root = strings.TrimRight(p[0], "\r")
			if len(p) == 2 {
				loc.prefix = strings.TrimSpace(p[1])
			}
		}
	}
	gitMu.Lock()
	repoRoots[dir] = loc
	gitMu.Unlock()
	return loc
}

// linkedWorktrees returns every linked worktree of the given repos (not the main
// checkouts, which are already covered), deduped and in a stable order. Results
// are cached for a bit since --watch rescans every second.
func linkedWorktrees(repos map[string]bool) []Worktree {
	var dirs []string
	for r := range repos {
		dirs = append(dirs, r)
	}
	sort.Strings(dirs)
	seen := map[string]bool{}
	var out []Worktree
	for _, r := range dirs {
		for _, wt := range worktreesOf(r) {
			if !seen[wt.Path] {
				seen[wt.Path] = true
				out = append(out, wt)
			}
		}
	}
	return out
}

func worktreesOf(repo string) []Worktree {
	gitMu.Lock()
	c, ok := wtCache[repo]
	gitMu.Unlock()
	if ok && time.Since(c.at) < 15*time.Second {
		return c.wts
	}
	out, err := git(repo, "worktree", "list", "--porcelain")
	var wts []Worktree
	if err == nil {
		wts = parseWorktrees(out)
	}
	gitMu.Lock()
	wtCache[repo] = wtEntry{time.Now(), wts}
	gitMu.Unlock()
	return wts
}

// parseWorktrees reads `git worktree list --porcelain`. The first entry is the
// main checkout and is skipped. Labels look like "repo@branch".
func parseWorktrees(porcelain string) []Worktree {
	var out []Worktree
	main := ""
	for i, block := range strings.Split(strings.TrimSpace(porcelain), "\n\n") {
		path, branch, bare := "", "", false
		for _, ln := range strings.Split(block, "\n") {
			switch {
			case strings.HasPrefix(ln, "worktree "):
				path = strings.TrimPrefix(ln, "worktree ")
			case strings.HasPrefix(ln, "branch "):
				branch = strings.TrimPrefix(strings.TrimPrefix(ln, "branch "), "refs/heads/")
			case ln == "bare":
				bare = true
			}
		}
		if i == 0 {
			main = path
			continue
		}
		if path == "" || bare {
			continue
		}
		if st, err := os.Stat(path); err != nil || !st.IsDir() { // prunable: dir is gone
			continue
		}
		name := branch
		if name == "" {
			name = filepath.Base(path) // detached HEAD
		}
		out = append(out, Worktree{Path: path, Label: filepath.Base(main) + "@" + strings.ReplaceAll(name, "/", "-")})
	}
	return out
}

// loadDiff picks a random commit that touched f and returns up to 3 of its hunks,
// each trimmed to maxRows.
//
// preferLatest tries the newest commit first; with --watch the file just changed,
// so that's the commit worth showing.
func loadDiff(f SourceFile, maxRows int, rng *rand.Rand, preferLatest bool) (*CommitDiff, bool) {
	loc := repoLocOf(filepath.Dir(f.Abs))
	if loc.root == "" {
		return nil, false
	}
	root, rel := loc.root, loc.prefix+filepath.Base(f.Abs)
	log, err := git(root, "log", "--no-merges", "-n", "40", "--format=%h%x1f%s%x1f%ar", "--", rel)
	if err != nil {
		return nil, false
	}
	commits := strings.Split(strings.TrimSpace(log), "\n")
	newest := commits[0]
	// the oldest commit is usually the file being created, which is just one long
	// add; only fall back to it when there's nothing else
	first := commits[len(commits)-1]
	if len(commits) > 1 {
		commits = commits[:len(commits)-1]
	}
	rng.Shuffle(len(commits), func(i, j int) { commits[i], commits[j] = commits[j], commits[i] })
	commits = append(commits[:min(4, len(commits))], first)
	if preferLatest {
		commits = append([]string{newest}, commits...)
	}
	tried := map[string]bool{}
	for _, c := range commits {
		p := strings.Split(c, "\x1f")
		if len(p) != 3 || tried[p[0]] {
			continue
		}
		tried[p[0]] = true
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
		cur = cur[:min(len(cur), max(maxRows, 0))]
		changed := false
		for _, r := range cur { // only worth showing if a change survived the trim
			changed = changed || r.Kind != ' '
		}
		if changed {
			hunks = append(hunks, cur)
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
