package main

import (
	"flag"
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

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
	exclude := fs.String("exclude", "", "extra comma-separated directory names to skip")
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
	if len(dirs) == 0 {
		dirs = []string{"."}
	}

	die := func(f string, a ...any) { fmt.Fprintf(os.Stderr, "billboard: "+f+"\n", a...); os.Exit(1) }
	if _, cfg.Theme = themeByName(*theme); cfg.Theme < 0 {
		die("unknown theme %q (run with -h to list them)", *theme)
	}
	if *effect == "all" {
		for i := range effects {
			cfg.Effects = append(cfg.Effects, i)
		}
	} else {
		for _, n := range splitList(*effect) {
			i := effectByName(n)
			if i < 0 {
				die("unknown effect %q (run with -h to list them)", n)
			}
			cfg.Effects = append(cfg.Effects, i)
		}
	}
	cfg.Brightness = clamp01(cfg.Brightness)
	cfg.MaxLines = max(cfg.MaxLines, 5)
	cfg.FPS = max(5, min(cfg.FPS, 120))
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		die("needs to run in a terminal")
	}
	for _, d := range dirs {
		if st, err := os.Stat(d); err != nil || !st.IsDir() {
			die("%s is not a directory", d)
		}
	}

	files := scan(dirs, splitList(*exclude))
	if len(files) == 0 {
		die("no files found under %s", strings.Join(dirs, " "))
	}

	old, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		die("%v", err)
	}
	enterScreen()
	restored := false
	restore := func() {
		if !restored {
			restored = true
			leaveScreen()
			term.Restore(int(os.Stdin.Fd()), old)
		}
	}
	defer restore()

	hint := ""
	if *ext != "" {
		for _, e := range splitList(*ext) {
			if !strings.HasPrefix(e, ".") && strings.ToLower(e) == e {
				e = "." + e // "go" -> ".go"; "Makefile" stays as a bare filename
			}
			cfg.Exts = append(cfg.Exts, e)
		}
	} else {
		cfg.Exts = pick(countExts(files), themes[cfg.Theme])
		if cfg.Exts == nil {
			return
		}
		var short []string
		for _, e := range cfg.Exts {
			short = append(short, strings.TrimPrefix(e, "."))
		}
		hint = fmt.Sprintf("Next time, skip the picker with:\n  billboard --ext %s %s\n", strings.Join(short, ","), quoteArgs(dirs))
	}

	want := map[string]bool{}
	for _, e := range cfg.Exts {
		want[e] = true
	}
	var chosen []SourceFile
	for _, f := range files {
		if want[f.Ext] {
			chosen = append(chosen, f)
		}
	}
	if len(chosen) == 0 {
		restore()
		die("no files matching %s", strings.Join(cfg.Exts, ", "))
	}

	if *seed == 0 {
		*seed = time.Now().UnixNano()
	}
	r := &Runner{cfg: cfg, files: chosen, rng: rand.New(rand.NewSource(*seed))}
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
		if strings.ContainsAny(s, " '\"$") {
			s = "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
		}
		out = append(out, s)
	}
	return strings.Join(out, " ")
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
	rng   *rand.Rand
	s     *Screen

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

func (r *Runner) nextFile() bool {
	lay := r.layout()
	for tries := 0; tries < 200; tries++ {
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
		ei := r.effectSel
		if ei < 0 {
			ei = r.cfg.Effects[r.rng.Intn(len(r.cfg.Effects))]
		}
		spec := &effects[ei]
		if spec.Build != nil {
			if eff, doc, ok := spec.Build(f, lay, r.cfg, r.rng); ok {
				r.start(spec, eff, doc)
				return true
			}
			if r.effectSel >= 0 && tries < 150 {
				continue // forced to this effect: keep looking for a file it works on
			}
			spec = r.fallbackEffect()
			if r.effectSel >= 0 {
				r.flash(fmt.Sprintf("%s: nothing usable found, showing %s", effects[ei].Name, spec.Name))
			}
		}
		lines := load(f)
		if len(lines) == 0 {
			continue
		}
		ch, first := chunk(lines, spec.Lines(lay, r.cfg), r.rng)
		if len(ch) == 0 {
			continue
		}
		doc := &Doc{Path: f.Rel, Lines: ch, FirstLine: first}
		r.start(spec, spec.New(doc, lay, r.cfg, r.rng), doc)
		return true
	}
	return false
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

func (r *Runner) start(spec *EffectSpec, eff Effect, doc *Doc) {
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
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGHUP)

	if !r.nextFile() {
		return "couldn't read any of the matching files"
	}
	tick := time.NewTicker(time.Second / time.Duration(r.cfg.FPS))
	defer tick.Stop()
	last := time.Now()
	for {
		select {
		case k, ok := <-keys:
			if !ok || r.handleKey(k) {
				return ""
			}
			continue
		case <-winch:
			w, h, _ := term.GetSize(int(os.Stdout.Fd()))
			r.s.Resize(w, h)
			r.nextFile()
			continue
		case <-quit:
			return ""
		case now := <-tick.C:
			dt := now.Sub(last).Seconds()
			last = now
			if !r.paused {
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
	if r.spec.NoOutro {
		r.nextFile()
		return
	}
	if r.hold < r.cfg.Hold {
		r.hold += dt
		return
	}
	r.outro += dt
	if r.outro >= 0.7 {
		r.nextFile()
	}
}

func (r *Runner) handleKey(k []byte) (quit bool) {
	cfg := r.cfg
	switch s := string(k); {
	case s == "q" || s == "Q" || s == "\x03" || s == "\x1b":
		return true
	case s == "n" || s == "\x1b[C":
		r.nextFile()
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
		r.nextFile()
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
		r.nextFile()
	case s == "h":
		cfg.Header = !cfg.Header
		r.nextFile()
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
	if r.cfg.Header {
		lay := r.layout()
		path := r.doc.Path
		if max := s.W - 6; len(path) > max && max > 3 {
			path = "…" + path[len(path)-max+1:]
		}
		s.Text(lay.X-lay.Gutter, 1, path, th.Header, false)
		if info := r.doc.Info; info != "" {
			x := lay.X - lay.Gutter + len([]rune(path)) + 3
			if room := s.W - 3 - x; room > 8 {
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
	if r.help {
		r.drawHelp(th)
	}
	if time.Now().Before(r.msgUntil) {
		m := " " + r.msg + " "
		s.Text(s.W-len([]rune(m))-2, s.H-1, m, th.Header, true)
	}
	s.Flush()
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
		switch k := string(buf[:n]); k {
		case "q", "\x03", "\x1b":
			return nil
		case "\x1b[A", "k":
			cur = max(0, cur-1)
		case "\x1b[B", "j":
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
