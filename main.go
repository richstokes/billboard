package main

import (
	"flag"
	"fmt"
	"math"
	"math/rand"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"golang.org/x/term"
)

type Config struct {
	Exts        []string
	Effects     []int // indexes into effects; picked from at random per file
	Theme       int
	Speed       float64
	CPS         float64
	LPS         float64
	Hold        float64
	MaxLines    int
	LineNumbers bool
	Header      bool
	Typos       bool
	Brightness  float64
	Transparent bool
	Ordered     bool
	FPS         int
	Watch       bool
}

const usage = `billboard - stream your source code as a live background

Usage:
  billboard [flags] [dir ...]

With no --ext, an interactive picker lists the file types found under the dirs.

Flags:
%s
Effects:
%s
Themes:
  %s

Keys while running:
  n / →      next file          space   pause
  e          cycle effect       t       cycle theme
  + / -      faster / slower    b / B   dimmer / brighter
  l          line numbers       h       header
  ?          show these keys    q       quit
`

func main() {
	cfg := &Config{}
	fs := flag.NewFlagSet("billboard", flag.ExitOnError)
	ext := fs.String("ext", "", "comma-separated extensions to include, e.g. go,ts,py (skips the picker)")
	effect := fs.String("effect", "all", "effect name, comma-separated list, or 'all' to mix")
	theme := fs.String("theme", "dracula", "color theme")
	fs.Float64Var(&cfg.Speed, "speed", 1, "global speed multiplier")
	fs.Float64Var(&cfg.CPS, "cps", 45, "typewriter characters per second")
	fs.Float64Var(&cfg.LPS, "lps", 3, "scroll lines per second")
	fs.Float64Var(&cfg.Hold, "hold", 4, "seconds to hold a finished file before moving on")
	fs.IntVar(&cfg.MaxLines, "max-lines", 60, "max lines typed per file (scroll shows 3x this)")
	fs.BoolVar(&cfg.LineNumbers, "line-numbers", true, "show line numbers")
	fs.BoolVar(&cfg.Header, "header", true, "show the file path header")
	fs.BoolVar(&cfg.Typos, "typos", true, "typewriter makes the odd typo and corrects it")
	fs.Float64Var(&cfg.Brightness, "brightness", 1, "0-1, dims everything toward the background")
	fs.BoolVar(&cfg.Transparent, "transparent", false, "don't paint a background (use the terminal's own)")
	fs.BoolVar(&cfg.Ordered, "ordered", false, "go through files in order instead of shuffling")
	fs.IntVar(&cfg.FPS, "fps", 30, "frames per second")
	fs.BoolVar(&cfg.Watch, "watch", false, "start blank and only show files as they're added or modified")
	exclude := fs.String("exclude", "", "extra comma-separated directory names to skip")
	worktrees := fs.Bool("include-worktrees", false, "also scan linked git worktrees of any repo under the dirs")
	repo := fs.String("repo", "", "git URL or GitHub owner/name to clone and show; dirs become paths inside it")
	fresh := fs.Bool("fresh", false, "with --repo, delete the local clone and clone again from scratch")
	repoEvery := fs.Duration("repo-interval", 10*time.Second, "with --repo and --watch, how often to pull new commits")
	seed := fs.Int64("seed", 0, "random seed (0 = random)")
	fs.Usage = func() {
		var fl, ef, th strings.Builder
		fs.VisitAll(func(f *flag.Flag) {
			def := ""
			if f.DefValue != "" && f.DefValue != "false" {
				def = " (default " + f.DefValue + ")"
			}
			fmt.Fprintf(&fl, "  --%-14s %s%s\n", f.Name, f.Usage, def)
		})
		for _, e := range effects {
			fmt.Fprintf(&ef, "  %-12s %s\n", e.Name, e.Desc)
		}
		var names []string
		for _, t := range themes {
			names = append(names, t.Name)
		}
		th.WriteString(strings.Join(names, ", "))
		fmt.Fprintf(os.Stderr, usage, fl.String(), ef.String(), th.String())
	}
	fs.Parse(reorderArgs(fs, os.Args[1:]))
	dirs := fs.Args()
	hintArgs := quoteArgs(dirs)
	if len(dirs) == 0 && *repo == "" {
		dirs = []string{"."}
	}

	die := func(f string, a ...any) { fmt.Fprintf(os.Stderr, "billboard: "+f+"\n", a...); os.Exit(1) }
	if _, cfg.Theme = themeByName(strings.ToLower(*theme)); cfg.Theme < 0 {
		die("unknown theme %q (run with -h to list them)", *theme)
	}
	for _, n := range splitList(strings.ToLower(*effect)) {
		if n == "all" {
			cfg.Effects = cfg.Effects[:0]
			for i := range effects {
				cfg.Effects = append(cfg.Effects, i)
			}
			break
		}
		i := effectByName(n)
		if i < 0 {
			die("unknown effect %q (run with -h to list them)", n)
		}
		cfg.Effects = append(cfg.Effects, i)
	}
	if len(cfg.Effects) == 0 {
		die("--effect needs at least one effect (run with -h to list them)")
	}
	// zero, negative or NaN rates freeze the effects or spin them forever
	for _, f := range []struct {
		name string
		v    float64
	}{{"speed", cfg.Speed}, {"cps", cfg.CPS}, {"lps", cfg.LPS}} {
		if !(f.v > 0) || math.IsInf(f.v, 0) {
			die("--%s must be greater than 0", f.name)
		}
	}
	if !(cfg.Hold >= 0) {
		die("--hold can't be negative")
	}
	if math.IsNaN(cfg.Brightness) {
		die("--brightness must be between 0 and 1")
	}
	cfg.Brightness = clamp01(cfg.Brightness)
	cfg.MaxLines = max(cfg.MaxLines, 5)
	cfg.FPS = max(5, min(cfg.FPS, 120))
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		die("needs to run in a terminal")
	}
	repoDir := ""
	if *repo != "" {
		url := normalizeRepoURL(*repo)
		var err error
		if repoDir, err = syncRepo(url, *fresh); err != nil {
			die("%v", err)
		}
		hintArgs = strings.TrimSpace("--repo " + quoteArgs([]string{*repo}) + " " + hintArgs)
		if len(dirs) == 0 {
			dirs = []string{repoDir}
		} else {
			for i, d := range dirs {
				dirs[i] = filepath.Join(repoDir, d)
				if rel, err := filepath.Rel(repoDir, dirs[i]); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
					die("%s is outside the repo", d)
				}
			}
		}
	}
	for _, d := range dirs {
		if st, err := os.Stat(d); err != nil || !st.IsDir() {
			die("%s is not a directory", d)
		}
	}

	opts := ScanOpts{Exclude: splitList(*exclude), Worktrees: *worktrees}
	files := scan(dirs, opts)
	if len(files) == 0 && !(cfg.Watch && *ext != "") {
		if cfg.Watch {
			die("no files under %s yet, so nothing to pick from; pass --ext too", strings.Join(dirs, " "))
		}
		die("no files found under %s", strings.Join(dirs, " "))
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGHUP)
	old, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		die("%v", err)
	}
	enterScreen()
	var once sync.Once
	restore := func() {
		once.Do(func() {
			leaveScreen()
			term.Restore(int(os.Stdin.Fd()), old)
		})
	}
	defer restore()
	// The run loop handles signals itself. Until it starts (the picker blocks on
	// stdin), make sure being killed still puts the terminal back.
	running := make(chan struct{})
	go func() {
		select {
		case <-quit:
			restore()
			os.Exit(1)
		case <-running:
		}
	}()

	hint := ""
	want := map[string]bool{}
	if *ext != "" {
		for _, e := range splitList(*ext) {
			cfg.Exts = append(cfg.Exts, e)
			want[e] = true // a bare filename as typed: Makefile, justfile
			want["."+strings.ToLower(strings.TrimPrefix(e, "."))] = true
		}
	} else {
		cfg.Exts = pick(countExts(files), themes[cfg.Theme])
		if cfg.Exts == nil {
			return
		}
		var short []string
		for _, e := range cfg.Exts {
			want[e] = true
			short = append(short, strings.TrimPrefix(e, "."))
		}
		watchFlag := ""
		if cfg.Watch {
			watchFlag = "--watch "
		}
		if *worktrees {
			watchFlag += "--include-worktrees "
		}
		hint = fmt.Sprintf("Next time, skip the picker with:\n  billboard %s--ext %s %s\n", watchFlag, quoteArgs([]string{strings.Join(short, ",")}), hintArgs)
	}

	var chosen []SourceFile
	for _, f := range files {
		if want[f.Ext] {
			chosen = append(chosen, f)
		}
	}
	if len(chosen) == 0 && !cfg.Watch {
		restore()
		die("no files matching %s", strings.Join(cfg.Exts, ", "))
	}

	if *seed == 0 {
		*seed = time.Now().UnixNano()
	}
	r := &Runner{cfg: cfg, files: chosen, dirs: dirs, repo: *repo, quit: quit, rng: rand.New(rand.NewSource(*seed))}
	if cfg.Watch {
		r.watch = newWatcher(dirs, opts, want, chosen)
		go r.watch.run(time.Second)
		if repoDir != "" {
			go pollRepo(repoDir, max(*repoEvery, 2*time.Second))
		}
	}
	close(running)
	if msg := r.Run(); msg != "" {
		restore()
		die("%s", msg)
	}
	if hint != "" {
		restore()
		fmt.Print(hint)
		os.Exit(0)
	}
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func quoteArgs(a []string) string {
	var out []string
	for _, s := range a {
		if s == "" || strings.IndexFunc(s, shellUnsafe) >= 0 {
			s = "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
		}
		out = append(out, s)
	}
	return strings.Join(out, " ")
}

func shellUnsafe(r rune) bool {
	return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("_-./,:@%+=", r))
}

// splitKeys breaks one read from the terminal into keys. Key repeat or a busy
// frame can deliver several at once ("++", or an arrow followed by a letter).
func splitKeys(b []byte) []string {
	var out []string
	for len(b) > 0 {
		n := 1
		switch {
		case b[0] == 0x1b && len(b) > 1 && (b[1] == '[' || b[1] == 'O'): // CSI / SS3: runs to a final byte
			for n = 2; n < len(b) && (b[n] < 0x40 || b[n] > 0x7e); n++ {
			}
			n = min(n+1, len(b))
		case b[0] == 0x1b && len(b) > 1: // alt+key; kept whole so it isn't read as esc
			n = 2
		default:
			_, n = utf8.DecodeRune(b)
		}
		out = append(out, string(b[:n]))
		b = b[n:]
	}
	return out
}

// reorderArgs lets flags come after positional args (billboard ~/code --ext go).
func reorderArgs(fs *flag.FlagSet, args []string) []string {
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			pos = append(pos, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			pos = append(pos, a)
			continue
		}
		flags = append(flags, a)
		name := strings.TrimLeft(a, "-")
		if strings.Contains(name, "=") {
			continue
		}
		if f := fs.Lookup(name); f != nil {
			if b, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && b.IsBoolFlag() {
				continue
			}
			if i+1 < len(args) {
				flags = append(flags, args[i+1])
				i++
			}
		}
	}
	return append(flags, pos...)
}

// ---- runner ----

type Runner struct {
	cfg   *Config
	files []SourceFile
	dirs  []string
	repo  string // --repo as given, for the idle screen
	quit  <-chan os.Signal
	watch *watcher // nil unless --watch

	curFile SourceFile
	curInfo string
	rng     *rand.Rand
	s       *Screen

	bag       []int
	effectSel int // -1 = random from cfg.Effects, else forced index
	cur       Effect
	spec      *EffectSpec
	doc       *Doc
	hold      float64
	outro     float64
	paused    bool
	msg       string
	msgUntil  time.Time
	help      bool
}

func (r *Runner) layout() Layout {
	top := 1
	if r.cfg.Header {
		top = 3
	}
	gutter := 0
	if r.cfg.LineNumbers {
		gutter = 7
	}
	x := 3 + gutter
	return Layout{X: x, Y: top, W: max(10, r.s.W-x-2), H: max(3, r.s.H-top-1), Gutter: gutter}
}

// takeFile returns the next file to show: from the change queue with --watch,
// otherwise from a shuffle bag so every file plays once before any repeats.
func (r *Runner) takeFile() (SourceFile, string, bool) {
	if r.watch != nil {
		it, ok := r.watch.pop()
		if !ok {
			return SourceFile{}, "", false
		}
		if it.isNew {
			return it.f, "new file", true
		}
		return it.f, "modified", true
	}
	if len(r.bag) == 0 {
		r.bag = make([]int, len(r.files))
		for i := range r.bag {
			r.bag[i] = i
		}
		if !r.cfg.Ordered {
			r.rng.Shuffle(len(r.bag), func(i, j int) { r.bag[i], r.bag[j] = r.bag[j], r.bag[i] })
		}
	}
	f := r.files[r.bag[0]]
	r.bag = r.bag[1:]
	return f, "", true
}

func (r *Runner) nextFile() bool {
	for tries := 0; tries < 200; tries++ {
		f, info, ok := r.takeFile()
		if !ok {
			return false
		}
		if r.play(f, info, tries) {
			return true
		}
	}
	return false
}

// replay restarts the current file, e.g. after changing effect or layout.
func (r *Runner) replay() {
	if r.cur == nil || !r.play(r.curFile, r.curInfo, 1<<30) {
		r.nextFile()
	}
}

// play starts f with the selected effect. False means f can't be shown.
func (r *Runner) play(f SourceFile, info string, tries int) bool {
	lay := r.layout()
	ei := r.effectSel
	if ei < 0 {
		ei = r.cfg.Effects[r.rng.Intn(len(r.cfg.Effects))]
	}
	spec := &effects[ei]
	if spec.Build != nil {
		if eff, doc, ok := spec.Build(f, lay, r.cfg, r.rng); ok {
			r.start(f, info, spec, eff, doc)
			return true
		}
		if r.effectSel >= 0 && r.watch == nil && tries < 150 {
			return false // forced to this effect: keep looking for a file it works on
		}
		spec = r.fallbackEffect()
		if r.effectSel >= 0 && r.watch == nil {
			r.flash(fmt.Sprintf("%s: nothing usable found, showing %s", effects[ei].Name, spec.Name))
		}
	}
	lines := load(f)
	if len(lines) == 0 {
		return false
	}
	ch, first := chunk(lines, spec.Lines(lay, r.cfg), r.rng)
	if len(ch) == 0 {
		return false
	}
	doc := &Doc{Path: f.Rel, Lines: ch, FirstLine: first}
	r.start(f, info, spec, spec.New(doc, lay, r.cfg, r.rng), doc)
	return true
}

// fallbackEffect picks a random selected effect that works on plain files.
func (r *Runner) fallbackEffect() *EffectSpec {
	var ok []int
	for _, i := range r.cfg.Effects {
		if effects[i].Build == nil {
			ok = append(ok, i)
		}
	}
	if len(ok) == 0 {
		return &effects[0]
	}
	return &effects[ok[r.rng.Intn(len(ok))]]
}

func (r *Runner) start(f SourceFile, info string, spec *EffectSpec, eff Effect, doc *Doc) {
	if doc.Info == "" {
		doc.Info = info
	}
	r.curFile, r.curInfo = f, info
	r.spec, r.cur, r.doc = spec, eff, doc
	r.hold, r.outro = 0, 0
}

func (r *Runner) flash(m string) {
	r.msg = m
	r.msgUntil = time.Now().Add(1800 * time.Millisecond)
}

func (r *Runner) Run() string {
	w, h, _ := term.GetSize(int(os.Stdout.Fd()))
	r.s = NewScreen(w, h)
	r.s.Transparent = r.cfg.Transparent
	r.effectSel = -1
	if len(r.cfg.Effects) == 1 {
		r.effectSel = r.cfg.Effects[0]
	}

	keys := make(chan []byte, 16)
	go func() {
		buf := make([]byte, 64)
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil {
				close(keys)
				return
			}
			keys <- append([]byte{}, buf[:n]...)
		}
	}()
	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)

	if r.watch == nil && !r.nextFile() {
		return "couldn't read any of the matching files"
	}
	tick := time.NewTicker(time.Second / time.Duration(r.cfg.FPS))
	defer tick.Stop()
	last := time.Now()
	for {
		select {
		case k, ok := <-keys:
			if !ok {
				return ""
			}
			for _, key := range splitKeys(k) {
				if r.handleKey(key) {
					return ""
				}
			}
			continue
		case <-winch:
			w, h, _ := term.GetSize(int(os.Stdout.Fd()))
			r.s.Resize(w, h)
			r.replay()
			continue
		case <-r.quit:
			return ""
		case now := <-tick.C:
			// capped so a stall (slow git call, laptop sleep) doesn't fast-forward the effect
			dt := min(now.Sub(last).Seconds(), 0.25)
			last = now
			if r.cur == nil && r.watch != nil {
				r.nextFile() // idle: pick up the first change as soon as it lands
			}
			if !r.paused && r.cur != nil {
				r.advance(dt)
			}
			r.draw()
		}
	}
}

func (r *Runner) advance(dt float64) {
	r.cur.Update(dt * r.cfg.Speed)
	if !r.cur.Done() {
		return
	}
	if r.spec.NoOutro && r.watch == nil {
		r.nextFile()
		return
	}
	if r.hold < r.cfg.Hold {
		r.hold += dt
		return
	}
	if r.watch != nil && r.watch.pending() == 0 {
		return // --watch: stay on this file until something else changes
	}
	r.outro += dt
	if r.outro >= 0.7 && !r.nextFile() {
		r.outro = 0
	}
}

func (r *Runner) handleKey(s string) (quit bool) {
	cfg := r.cfg
	switch {
	case s == "q" || s == "Q" || s == "\x03" || s == "\x1b":
		return true
	case s == "n" || s == "\x1b[C" || s == "\x1bOC":
		if r.watch != nil && r.watch.pending() == 0 {
			r.flash("no changes queued")
		} else {
			r.nextFile()
		}
	case s == " ":
		r.paused = !r.paused
		r.flash(map[bool]string{true: "paused", false: "resumed"}[r.paused])
	case s == "e":
		r.effectSel++
		if r.effectSel >= len(effects) {
			r.effectSel = -1
		}
		if r.effectSel < 0 {
			r.flash("effect: mix")
		} else {
			r.flash("effect: " + effects[r.effectSel].Name)
		}
		r.replay()
	case s == "t":
		cfg.Theme = (cfg.Theme + 1) % len(themes)
		r.flash("theme: " + themes[cfg.Theme].Name)
	case s == "T":
		cfg.Theme = (cfg.Theme + len(themes) - 1) % len(themes)
		r.flash("theme: " + themes[cfg.Theme].Name)
	case s == "+" || s == "=":
		cfg.Speed = min(cfg.Speed*1.25, 20)
		r.flash(fmt.Sprintf("speed: %.2fx", cfg.Speed))
	case s == "-" || s == "_":
		cfg.Speed = max(cfg.Speed/1.25, 0.05)
		r.flash(fmt.Sprintf("speed: %.2fx", cfg.Speed))
	case s == "b":
		cfg.Brightness = max(0.1, cfg.Brightness-0.1)
		r.flash(fmt.Sprintf("brightness: %.0f%%", cfg.Brightness*100))
	case s == "B":
		cfg.Brightness = min(1, cfg.Brightness+0.1)
		r.flash(fmt.Sprintf("brightness: %.0f%%", cfg.Brightness*100))
	case s == "l":
		cfg.LineNumbers = !cfg.LineNumbers
		r.replay()
	case s == "h":
		cfg.Header = !cfg.Header
		r.replay()
	case s == "?":
		r.help = !r.help
	}
	return false
}

func (r *Runner) draw() {
	th := themes[r.cfg.Theme]
	s := r.s
	s.BG = th.BG
	s.Brightness = r.cfg.Brightness
	if r.outro > 0 {
		s.Brightness *= 1 - clamp01(r.outro/0.7)
	}
	s.Clear()
	if r.cur == nil {
		r.drawIdle(th)
	} else {
		r.drawFile(th)
	}
	if r.help {
		r.drawHelp(th)
	}
	if time.Now().Before(r.msgUntil) {
		m := " " + r.msg + " "
		s.Text(s.W-len([]rune(m))-2, s.H-1, m, th.Header, true)
	}
	s.Flush()
}

// drawIdle is the --watch screen before anything has changed.
func (r *Runner) drawIdle(th *Theme) {
	s := r.s
	t := float64(time.Now().UnixMilli()%100000) / 1000
	msg := "watching " + strings.Join(r.dirs, ", ")
	if len([]rune(msg)) > s.W-4 {
		msg = "watching for changes"
	}
	dots := strings.Repeat(".", int(t*2)%4)
	wait := "waiting for a file to be saved"
	if r.repo != "" {
		msg, wait = "watching "+r.repo, "waiting for new commits"
	}
	sub := wait + dots + strings.Repeat(" ", 3-len(dots))
	y := s.H/2 - 1
	pulse := 0.55 + 0.25*math.Sin(t*2)
	s.Text((s.W-len([]rune(msg)))/2, y, msg, lerp(th.BG, th.Header, pulse), false)
	s.Text((s.W-len([]rune(sub)))/2, y+2, sub, lerp(th.BG, th.LineNr, 0.9), false)
}

func (r *Runner) drawFile(th *Theme) {
	s := r.s
	if r.cfg.Header {
		lay := r.layout()
		queued := ""
		if r.watch != nil {
			if n := r.watch.pending(); n > 0 {
				queued = fmt.Sprintf("+%d queued", n)
				s.Text(s.W-3-len(queued), 1, queued, th.Header, false)
			}
		}
		path := r.doc.Path
		if pr, room := []rune(path), s.W-6; len(pr) > room && room > 3 {
			path = "…" + string(pr[len(pr)-room+1:])
		}
		s.Text(lay.X-lay.Gutter, 1, path, th.Header, false)
		if info := r.doc.Info; info != "" {
			x := lay.X - lay.Gutter + len([]rune(path)) + 3
			if room := s.W - 3 - x - len(queued) - 2; room > 8 {
				if len([]rune(info)) > room {
					info = string([]rune(info)[:room-1]) + "…"
				}
				s.Text(x, 1, info, lerp(th.LineNr, th.Header, 0.6), false)
			}
		}
		sep := lerp(th.BG, th.LineNr, 0.7)
		for x := lay.X - lay.Gutter; x < s.W-2; x++ {
			s.Set(x, 2, '─', sep, false)
		}
	}
	r.cur.Draw(s, th)
}

func (r *Runner) drawHelp(th *Theme) {
	lines := []string{
		"n / →   next file", "space   pause", "e       cycle effect", "t / T   cycle theme",
		"+ / -   speed", "b / B   dimmer / brighter", "l       line numbers", "h       header",
		"?       hide this", "q       quit",
	}
	w := 30
	x0, y0 := r.s.W-w-3, 4
	for i := -1; i <= len(lines); i++ {
		for x := x0 - 2; x < x0+w; x++ {
			r.s.Set(x, y0+i, ' ', th.BG, false)
		}
	}
	for i, l := range lines {
		r.s.Text(x0, y0+i, l, th.FG, false)
	}
}

// ---- extension picker ----

func pick(exts []ExtCount, th *Theme) []string {
	sel := make([]bool, len(exts))
	for i, e := range exts {
		sel[i] = e.Default
	}
	cur, off := 0, 0
	out := os.Stdout
	buf := make([]byte, 16)
	for {
		w, h, _ := term.GetSize(int(out.Fd()))
		rows := max(3, h-6)
		if cur < off {
			off = cur
		}
		if cur >= off+rows {
			off = cur - rows + 1
		}
		var b strings.Builder
		fg := func(c RGB) string { return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", c.R, c.G, c.B) }
		fmt.Fprintf(&b, "\x1b[0m\x1b[48;2;%d;%d;%dm\x1b[2J\x1b[H", th.BG.R, th.BG.G, th.BG.B)
		fmt.Fprintf(&b, "\r\n  %s\x1b[1mWhich file types should billboard stream?\x1b[22m\r\n", fg(th.FG))
		fmt.Fprintf(&b, "  %s↑/↓ move · space toggle · a all · x none · enter start · q quit\r\n\r\n", fg(th.Header))
		for i := off; i < min(len(exts), off+rows); i++ {
			e := exts[i]
			box, col := "[ ]", th.Header
			if sel[i] {
				box, col = "[x]", th.Syntax[KFunction]
			}
			ptr := "  "
			if i == cur {
				ptr = fg(th.Cursor) + "> "
			}
			label := e.Ext
			fmt.Fprintf(&b, "  %s%s%s %-*s %s%5d files\x1b[K\r\n", ptr, fg(col), box, min(30, w-20), label, fg(th.LineNr), e.Count)
		}
		out.WriteString(b.String())

		n, err := os.Stdin.Read(buf)
		if err != nil {
			return nil
		}
		for _, k := range splitKeys(buf[:n]) {
			switch k {
			case "q", "\x03", "\x1b":
				return nil
			case "\x1b[A", "\x1bOA", "k":
				cur = max(0, cur-1)
			case "\x1b[B", "\x1bOB", "j":
				cur = min(len(exts)-1, cur+1)
			case " ", "\t":
				sel[cur] = !sel[cur]
				cur = min(len(exts)-1, cur+1)
			case "a":
				for i := range sel {
					sel[i] = true
				}
			case "x":
				for i := range sel {
					sel[i] = false
				}
			case "\r", "\n":
				var outExts []string
				for i, s := range sel {
					if s {
						outExts = append(outExts, exts[i].Ext)
					}
				}
				if len(outExts) > 0 {
					return outExts
				}
			}
		}
	}
}
