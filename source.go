package main

import (
	"bytes"
	"io/fs"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/mattn/go-runewidth"
)

type Glyph struct {
	Ch   rune
	Kind Kind
}

type Line []Glyph

// Doc is the chunk of a file currently on screen.
type Doc struct {
	Path      string // display path
	Lines     []Line
	FirstLine int    // 1-based line number of Lines[0]
	Info      string // extra header text, e.g. the commit for diff
}

var skipDirs = map[string]bool{
	"node_modules": true, "vendor": true, "dist": true, "build": true, "target": true,
	"__pycache__": true, "venv": true, "coverage": true, "bin": true, "obj": true,
	"Pods": true, "DerivedData": true, "out": true, "tmp": true, "deps": true, "_build": true,
}

var skipFiles = map[string]bool{
	"package-lock.json": true, "yarn.lock": true, "pnpm-lock.yaml": true, "go.sum": true,
	"Cargo.lock": true, "poetry.lock": true, "Gemfile.lock": true, "composer.lock": true,
	"bun.lockb": true, "uv.lock": true,
}

// Extensions picked by default in the picker: things that read as "code".
var notCode = map[string]bool{
	".json": true, ".md": true, ".txt": true, ".csv": true, ".svg": true, ".xml": true,
	".lock": true, ".sum": true, ".mod": true, ".sh": true, ".cmd": true, ".bat": true, ".ps1": true, ".map": true, ".log": true, ".tsv": true, ".snap": true,
	".min.js": true, ".ini": true, ".cfg": true,
}

const maxFileSize = 512 * 1024

func extOf(path string) string {
	base := filepath.Base(path)
	lower := strings.ToLower(base)
	if strings.HasSuffix(lower, ".min.js") {
		return ".min.js"
	}
	if strings.HasSuffix(lower, ".d.ts") {
		return ".d.ts"
	}
	if e := filepath.Ext(base); e != "" && e != base {
		return strings.ToLower(e)
	}
	return base // Makefile, Dockerfile, etc.
}

type SourceFile struct {
	Abs, Rel, Ext string
	ModTime       int64 // unix nanos, used by --watch
	Size          int64
}

type ScanOpts struct {
	Exclude   []string // extra dir names to skip
	Worktrees bool     // also scan linked git worktrees of any repo found
}

func scan(dirs []string, o ScanOpts) []SourceFile {
	skip := map[string]bool{}
	for k := range skipDirs {
		skip[k] = true
	}
	for _, s := range o.Exclude {
		skip[s] = true
	}
	var out []SourceFile
	seen := map[string]bool{}  // files, so a worktree inside a scanned dir isn't shown twice
	repos := map[string]bool{} // repo dirs spotted on the way, for --include-worktrees
	walk := func(root, label string) {
		filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			name := d.Name()
			if name == ".git" && o.Worktrees { // dir in a normal checkout, file in a linked worktree
				repos[filepath.Dir(p)] = true
			}
			if d.IsDir() {
				if p != root && (strings.HasPrefix(name, ".") || skip[name] || isSecretDir(name)) {
					return filepath.SkipDir
				}
				return nil
			}
			if !d.Type().IsRegular() || strings.HasPrefix(name, ".") || skipFiles[name] || isSecretFile(name) || seen[p] {
				return nil
			}
			info, err := d.Info()
			if err != nil || info.Size() == 0 || info.Size() > maxFileSize {
				return nil
			}
			seen[p] = true
			rel, _ := filepath.Rel(root, p)
			out = append(out, SourceFile{Abs: p, Rel: filepath.Join(label, rel), Ext: extOf(name),
				ModTime: info.ModTime().UnixNano(), Size: info.Size()})
			return nil
		})
	}
	for _, d := range dirs {
		root, _ := filepath.Abs(d)
		label := filepath.Base(root)
		// git reports worktree paths with symlinks resolved, so walk the real path
		// too or a worktree under a symlinked dir gets listed twice
		if real, err := filepath.EvalSymlinks(root); err == nil {
			root = real
		}
		walk(root, label)
		if o.Worktrees {
			if r := repoRoot(root); r != "" { // dir is somewhere inside a repo
				repos[r] = true
			}
		}
	}
	if o.Worktrees {
		for _, wt := range linkedWorktrees(repos) {
			walk(wt.Path, wt.Label)
		}
	}
	return out
}

type ExtCount struct {
	Ext     string
	Count   int
	Default bool
}

func countExts(files []SourceFile) []ExtCount {
	m := map[string]int{}
	for _, f := range files {
		m[f.Ext]++
	}
	var out []ExtCount
	for e, n := range m {
		isCode := !notCode[e] && strings.HasPrefix(e, ".") && lexers.Match("x"+e) != nil
		out = append(out, ExtCount{e, n, isCode})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Ext < out[j].Ext
	})
	return out
}

func kindOf(t chroma.TokenType) Kind {
	switch {
	case t.InCategory(chroma.Comment):
		return KComment
	case t == chroma.KeywordType:
		return KType
	case t.InCategory(chroma.Keyword), t == chroma.NameTag:
		return KKeyword
	case t.InSubCategory(chroma.LiteralString):
		return KString
	case t.InSubCategory(chroma.LiteralNumber), t == chroma.KeywordConstant:
		return KNumber
	case t == chroma.NameFunction, t == chroma.NameFunctionMagic, t == chroma.NameAttribute:
		return KFunction
	case t == chroma.NameClass, t == chroma.NameNamespace, t == chroma.NameException:
		return KType
	case t == chroma.NameBuiltin, t == chroma.NameBuiltinPseudo, t == chroma.NameDecorator, t == chroma.NameConstant:
		return KBuiltin
	case t.InCategory(chroma.Operator):
		return KOperator
	case t.InCategory(chroma.Punctuation):
		return KPunct
	}
	return KText
}

// load reads and highlights a file. Returns nil for binary/unreadable files.
func load(f SourceFile) []Line {
	data, err := os.ReadFile(f.Abs)
	if err != nil || bytes.IndexByte(data[:min(len(data), 8192)], 0) >= 0 {
		return nil
	}
	src := strings.ReplaceAll(string(data), "\r\n", "\n")
	if hasSecret(src) {
		return nil
	}
	return highlight(f.Abs, src)
}

// highlight tokenises src, picking the lexer from the file name.
func highlight(name, src string) []Line {
	src = strings.ReplaceAll(strings.TrimPrefix(src, "\ufeff"), "\r\n", "\n")
	lexer := lexers.Match(name)
	if lexer == nil {
		lexer = lexers.Analyse(src)
	}
	if lexer == nil {
		lexer = lexers.Fallback
	}
	lexer = chroma.Coalesce(lexer)
	it, err := lexer.Tokenise(nil, src)
	if err != nil {
		return nil
	}
	lines := []Line{{}}
	for tok := it(); tok != chroma.EOF; tok = it() {
		k := kindOf(tok.Type)
		for _, r := range tok.Value {
			cur := &lines[len(lines)-1]
			switch {
			case r == '\n':
				lines = append(lines, Line{})
			case r == '\t':
				for n := 4 - len(*cur)%4; n > 0; n-- {
					*cur = append(*cur, Glyph{' ', k})
				}
			case r < ' ' || r == 0x7f:
			case runewidth.RuneWidth(r) != 1:
				*cur = append(*cur, Glyph{'·', k})
			default:
				*cur = append(*cur, Glyph{r, k})
			}
		}
	}
	for i := range lines { // trim trailing whitespace
		l := lines[i]
		for len(l) > 0 && l[len(l)-1].Ch == ' ' {
			l = l[:len(l)-1]
		}
		lines[i] = l
	}
	return lines
}

// chunk picks up to n lines from a file, starting at the top of a block.
func chunk(lines []Line, n int, rng *rand.Rand) ([]Line, int) {
	start := 0
	if len(lines) > n {
		start = rng.Intn(len(lines) - n + 1)
		for back := 0; back < 25 && start > 0; back++ {
			if len(lines[start-1]) == 0 && len(lines[start]) > 0 {
				break
			}
			start--
		}
	}
	end := min(len(lines), start+n)
	for start < end && len(lines[start]) == 0 {
		start++
	}
	for end > start && len(lines[end-1]) == 0 {
		end--
	}
	return lines[start:end], start + 1
}
