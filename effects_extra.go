package main

import (
	"fmt"
	"math"
	"math/rand"
	"path/filepath"
	"strings"
)

func easeOut(p float64) float64 { p = clamp01(p); return 1 - math.Pow(1-p, 3) }

// ---- hack: glyphs fly in from everywhere, then a fake "hacking" overlay ----

type flyer struct {
	row, col int
	g        Glyph
	sx, sy   float64
	t0, dur  float64
}

type hack struct {
	d         *Doc
	l         Layout
	t, landed float64
	flyers    []flyer
	pool      []rune
	task, msg string
}

var hackTasks = []string{"BYPASSING FIREWALL", "DECRYPTING PAYLOAD", "INJECTING SHELLCODE", "CRACKING MAINFRAME",
	"REROUTING THROUGH 7 PROXIES", "COMPILING EXPLOIT", "SPOOFING CERTIFICATES", "DISABLING ICE"}
var hackResults = []string{"ACCESS GRANTED", "HACK THE PLANET", "ROOT ACCESS OBTAINED", "UPLOAD COMPLETE",
	"SYSTEM COMPROMISED", "I'M IN"}

const (
	hackGap      = 0.4
	hackProgress = 1.8
	hackFlash    = 1.8
)

func newHack(d *Doc, l Layout, c *Config, rng *rand.Rand) Effect {
	e := &hack{d: d, l: l, pool: glyphPool(d),
		task: hackTasks[rng.Intn(len(hackTasks))], msg: hackResults[rng.Intn(len(hackResults))]}
	for row, ln := range d.Lines {
		for col, g := range ln[:min(len(ln), l.W)] {
			if g.Ch == ' ' {
				continue
			}
			f := flyer{row: row, col: col, g: g,
				sx: rng.Float64()*float64(l.W+20) - 10, sy: rng.Float64()*float64(l.H+10) - 5,
				t0: float64(row)*0.035 + rng.Float64()*0.7, dur: 0.5 + rng.Float64()*0.6}
			e.flyers = append(e.flyers, f)
			e.landed = math.Max(e.landed, f.t0+f.dur)
		}
	}
	return e
}

func (e *hack) Update(dt float64) { e.t += dt }
func (e *hack) Done() bool        { return e.t > e.landed+hackGap+hackProgress+hackFlash }

func (e *hack) Draw(s *Screen, th *Theme) {
	for row := range e.d.Lines {
		if e.t > float64(row)*0.035+0.9 && row < e.l.H {
			drawGutter(s, th, e.l, row, e.d.FirstLine+row)
		}
	}
	for i, f := range e.flyers {
		p := (e.t - f.t0) / f.dur
		if p <= 0 {
			continue
		}
		k := easeOut(p)
		x := int(math.Round(f.sx + (float64(f.col)-f.sx)*k))
		y := int(math.Round(f.sy + (float64(f.row)-f.sy)*k))
		ch := f.g.Ch
		if p < 0.75 {
			ch = e.pool[hash(i, 7, int(e.t*15))%uint32(len(e.pool))]
		}
		if y < 0 || y >= e.l.H || x < -e.l.Gutter || x >= e.l.W {
			continue // keep off the header
		}
		s.Set(e.l.X+x, e.l.Y+y, ch, lerp(th.RainHot, th.Syntax[f.g.Kind], clamp01(p)), p < 1)
	}

	ot := e.t - e.landed - hackGap
	if ot < 0 || e.Done() {
		return
	}
	bw := max(len(e.task), len(e.msg)) + 14
	bh := 5
	bx, by := e.l.X+(e.l.W-bw)/2, e.l.Y+(e.l.H-bh)/2
	boxBG := lerp(th.BG, RGB{}, 0.6)
	s.Fill(bx, by, bw, bh, boxBG)
	border := th.Rain
	for x := bx; x < bx+bw; x++ {
		s.Set(x, by, '─', border, false)
		s.Set(x, by+bh-1, '─', border, false)
	}
	for y := by; y < by+bh; y++ {
		s.Set(bx, y, '│', border, false)
		s.Set(bx+bw-1, y, '│', border, false)
	}
	s.Set(bx, by, '┌', border, false)
	s.Set(bx+bw-1, by, '┐', border, false)
	s.Set(bx, by+bh-1, '└', border, false)
	s.Set(bx+bw-1, by+bh-1, '┘', border, false)

	if ot < hackProgress {
		// progress jumps in uneven steps, like a real progress bar that's lying to you
		p := clamp01(ot / hackProgress)
		p = math.Floor(p*12)/12 + hash01(int(p*12), 3, 9)*0.05
		p = clamp01(p)
		dots := strings.Repeat(".", int(ot*3)%4)
		s.Text(bx+3, by+1, e.task+dots, th.RainHot, true)
		barW := bw - 12
		fill := int(p * float64(barW))
		for i := 0; i < barW; i++ {
			ch, col := '░', lerp(boxBG, th.Rain, 0.5)
			if i < fill {
				ch, col = '█', th.Rain
			}
			s.Set(bx+3+i, by+3, ch, col, false)
		}
		s.Text(bx+bw-8, by+3, fmt.Sprintf("%3.0f%%", p*100), th.FG, false)
		return
	}
	on := int((ot-hackProgress)/0.22)%2 == 0
	fg, bg := th.RainHot, boxBG
	if on {
		fg, bg = th.BG, th.Rain
	}
	for y := by + 1; y < by+bh-1; y++ {
		for x := bx + 1; x < bx+bw-1; x++ {
			s.Set(x, y, ' ', bg, false)
			s.SetBG(x, y, bg)
		}
	}
	mx := bx + (bw-len(e.msg))/2
	for i, r := range e.msg {
		s.Set(mx+i, by+2, r, fg, true)
		s.SetBG(mx+i, by+2, bg)
	}
}

// ---- starfield: code characters at warp speed, real lines land one by one ----

type star struct {
	x, y, z float64
	ch      rune
	k       Kind
}

type starfield struct {
	d     *Doc
	l     Layout
	rng   *rand.Rand
	t     float64
	stars []star
	pool  []Glyph
}

const (
	starLandStart = 1.0
	starLandEvery = 0.3
	starLandDur   = 0.9
)

func newStarfield(d *Doc, l Layout, c *Config, rng *rand.Rand) Effect {
	e := &starfield{d: d, l: l, rng: rng}
	for _, ln := range d.Lines {
		for _, g := range ln {
			if g.Ch != ' ' {
				e.pool = append(e.pool, g)
			}
		}
	}
	if len(e.pool) == 0 {
		e.pool = []Glyph{{'*', KText}}
	}
	e.stars = make([]star, max(40, l.W*l.H/14))
	for i := range e.stars {
		e.spawn(&e.stars[i], 0.05+rng.Float64()*0.95)
	}
	return e
}

func (e *starfield) spawn(st *star, z float64) {
	g := e.pool[e.rng.Intn(len(e.pool))]
	*st = star{x: e.rng.Float64()*2 - 1, y: e.rng.Float64()*2 - 1, z: z, ch: g.Ch, k: g.Kind}
}

func (e *starfield) Update(dt float64) {
	e.t += dt
	for i := range e.stars {
		e.stars[i].z -= dt * 0.4
		if e.stars[i].z <= 0.03 {
			e.spawn(&e.stars[i], 1)
		}
	}
}

func (e *starfield) landAt(row int) float64 { return starLandStart + float64(row)*starLandEvery }

func (e *starfield) Done() bool {
	return e.t > e.landAt(min(len(e.d.Lines), e.l.H)-1)+starLandDur+0.5
}

func (e *starfield) project(x, y, z float64) (int, int) {
	cx, cy := float64(e.l.X)+float64(e.l.W)/2, float64(e.l.Y)+float64(e.l.H)/2
	return int(cx + x/z*float64(e.l.W)*0.09), int(cy + y/z*float64(e.l.H)*0.09)
}

func (e *starfield) Draw(s *Screen, th *Theme) {
	occupied := func(x, y int) bool { // keep stars out from behind landed code, and off the header
		row, col := y-e.l.Y, x-e.l.X
		if row < 0 || row >= e.l.H || col < -e.l.Gutter || col >= e.l.W {
			return true
		}
		if row < 0 || row >= len(e.d.Lines) || row >= e.l.H || e.t < e.landAt(row)+starLandDur {
			return false
		}
		return col >= -2 && col <= len(e.d.Lines[row])
	}
	for _, st := range e.stars {
		near := 1 - st.z
		x, y := e.project(st.x, st.y, st.z)
		if near > 0.75 { // streak behind the fast ones
			tx, ty := e.project(st.x, st.y, st.z+0.07)
			if !occupied(tx, ty) {
				s.Set(tx, ty, '·', lerp(th.BG, th.FG, near*0.35), false)
			}
		}
		if occupied(x, y) {
			continue
		}
		ch := '.'
		if near > 0.5 {
			ch = st.ch
		} else if near > 0.25 {
			ch = '·'
		}
		s.Set(x, y, ch, lerp(th.BG, lerp(th.FG, th.Syntax[st.k], 0.6), math.Pow(near, 1.4)*0.85), false)
	}
	cx, cy := float64(e.l.W)/2, float64(e.l.H)/2
	for row := 0; row < min(len(e.d.Lines), e.l.H); row++ {
		p := (e.t - e.landAt(row)) / starLandDur
		if p <= 0 {
			break
		}
		k := easeOut(p)
		if p >= 1 {
			drawGutter(s, th, e.l, row, e.d.FirstLine+row)
		}
		ln := e.d.Lines[row]
		for col := 0; col < min(len(ln), e.l.W); col++ {
			if ln[col].Ch == ' ' {
				continue
			}
			x := int(math.Round(cx + (float64(col)-cx)*k))
			y := int(math.Round(cy + (float64(row)-cy)*k))
			s.Set(e.l.X+x, e.l.Y+y, ln[col].Ch, lerp(th.BG, th.Syntax[ln[col].Kind], 0.3+0.7*k), false)
		}
	}
}

// ---- boot: a CRT warms up, a themed boot sequence runs, then the file is listed ----

type bootLine struct {
	text   string
	role   int     // 0 fg, 1 dim, 2 accent
	delay  float64 // pause before the line starts
	cmd    bool    // typed out as a command
	typed  int     // length of the prompt, which prints instantly
	count  int     // >0: animate the first %d in text from 0 up to this
	dur    float64
	code   int  // >0: 1-based index into doc lines
	clear  bool // wipe the screen before this line
	start  float64
	finish float64
}

type boot struct {
	d     *Doc
	l     Layout
	theme string
	t     float64
	lines []bootLine
	swirl bool
	end   float64
}

const (
	crtWarm   = 0.45
	swirlDur  = 2.6
	typeDelay = 0.065
)

func newBoot(d *Doc, l Layout, c *Config, rng *rand.Rand) Effect {
	e := &boot{d: d, l: l, theme: themes[c.Theme].Name}
	file := filepath.Base(d.Path)
	root := strings.SplitN(d.Path, string(filepath.Separator), 2)[0]
	L := func(text string, role int, delay float64) bootLine {
		return bootLine{text: text, role: role, delay: delay}
	}
	T := func(prompt, cmd string, delay float64) bootLine {
		return bootLine{text: prompt + cmd, cmd: true, typed: len([]rune(prompt)), delay: delay}
	}
	switch e.theme {
	case "c64":
		up := strings.ToUpper(file)
		e.lines = []bootLine{
			L("", 0, 0.3),
			L("    **** BILLBOARD 64 BASIC V2 ****", 0, 0),
			L("", 0, 0),
			L(" 64K RAM SYSTEM  38911 BASIC BYTES FREE", 0, 0),
			L("", 0, 0),
			L("READY.", 0, 0),
			T("", `LOAD"`+up+`",8,1`, 0.8),
			L("", 0, 0.3),
			L("SEARCHING FOR "+up, 0, 0),
			L("LOADING", 0, 1.3),
			L("READY.", 0, 2.0),
			T("", "LIST", 0.6),
			L("", 0, 0.3),
		}
	case "dreamcast":
		e.swirl = true
		e.lines = []bootLine{
			L("BILLBOARD ENTERTAINMENT SYSTEM", 2, 0),
			L("", 0, 0),
			L("Reading disc....", 1, 0.3),
			{text: "  Sectors read: %d", role: 1, count: 297000, dur: 1.0},
			L("  Volume: "+strings.ToUpper(root), 1, 0.2),
			L("", 0, 0),
			L("Starting "+file, 0, 0.6),
			L("", 0, 0.3),
		}
	case "matrix":
		e.lines = []bootLine{
			T("", "Wake up, Neo...", 0.6),
			{clear: true, delay: 1.8},
			T("", "The Matrix has you...", 0.4),
			{clear: true, delay: 1.8},
			T("", "Follow the white rabbit.", 0.4),
			{clear: true, delay: 1.8},
			T("", "Knock, knock.", 0.4),
			{clear: true, delay: 1.4},
		}
	default:
		e.lines = []bootLine{
			L("BILLBOARD BIOS v4.51PG, (C) 1984-2026 Billboard Systems Inc.", 2, 0.2),
			L("", 0, 0),
			L("CPU: 6502 compatible @ 4.77 MHz", 0, 0.3),
			{text: "Memory Test: %dK OK", count: 65536, dur: 1.2},
			L("", 0, 0),
			L("Detecting drives...", 0, 0.5),
			L("  Primary Master:   CODE0 "+root, 1, 0.7),
			L("  Primary Slave:    None", 1, 0.4),
			L("", 0, 0),
			L("Booting from CODE0...", 0, 0.8),
			L("", 0, 0.6),
			T("$ ", "cat "+d.Path, 0.4),
		}
	}
	for i := range d.Lines {
		e.lines = append(e.lines, bootLine{code: i + 1, delay: 1.0 / 30})
	}
	t := crtWarm
	if e.swirl {
		t += swirlDur
	}
	for i := range e.lines {
		b := &e.lines[i]
		t += b.delay
		b.start = t
		if b.cmd {
			t += float64(len([]rune(b.text))-b.typed) * typeDelay
		}
		t += b.dur
		b.finish = t
	}
	e.end = t
	return e
}

func (e *boot) Update(dt float64) { e.t += dt }
func (e *boot) Done() bool        { return e.t > e.end+0.5 }

func (e *boot) role(th *Theme, r int) RGB {
	switch r {
	case 1:
		return th.Header
	case 2:
		return th.Syntax[KKeyword]
	}
	return th.FG
}

func (e *boot) Draw(s *Screen, th *Theme) {
	if e.theme == "c64" { // the famous light blue border
		for x := 0; x < s.W; x++ {
			s.SetBG(x, 0, th.FG)
			s.SetBG(x, s.H-1, th.FG)
		}
		for y := 0; y < s.H; y++ {
			for _, x := range []int{0, 1, s.W - 2, s.W - 1} {
				s.SetBG(x, y, th.FG)
			}
		}
	}

	if e.t < crtWarm {
		e.warmup(s, th)
		return
	}
	if e.swirl && e.t < crtWarm+swirlDur {
		e.drawSwirl(s, th, e.t-crtWarm)
		return
	}

	// collect visible lines; a clear wipes everything before it
	var vis []*bootLine
	for i := range e.lines {
		b := &e.lines[i]
		if e.t < b.start {
			break
		}
		if b.clear {
			vis = vis[:0]
			continue
		}
		vis = append(vis, b)
	}
	top := max(0, len(vis)-e.l.H)
	left := e.l.X - e.l.Gutter
	var last *bootLine
	for row, b := range vis[top:] {
		y := e.l.Y + row
		last = b
		if b.code > 0 {
			drawGutter(s, th, e.l, row, e.d.FirstLine+b.code-1)
			ln := e.d.Lines[b.code-1]
			for c := 0; c < min(len(ln), e.l.W); c++ {
				s.Set(e.l.X+c, y, ln[c].Ch, th.Syntax[ln[c].Kind], false)
			}
			continue
		}
		text := []rune(b.text)
		if b.count > 0 {
			text = []rune(fmt.Sprintf(b.text, int(float64(b.count)*clamp01((e.t-b.start)/b.dur))))
		}
		if b.cmd {
			n := b.typed + int((e.t-b.start)/typeDelay)
			text = text[:min(len(text), n)]
		}
		s.Text(left, y, string(text), e.role(th, b.role), false)
	}
	// blinking block cursor at the end of the last line, until the code starts
	if last != nil && last.code == 0 && math.Mod(e.t, 0.8) < 0.45 {
		row := len(vis) - 1 - top
		n := len([]rune(last.text))
		if last.cmd {
			n = min(n, last.typed+int((e.t-last.start)/typeDelay))
		}
		if e.t > last.finish && !last.cmd {
			row, n = row+1, 0
		}
		s.Set(left+n, e.l.Y+row, '█', th.Cursor, false)
	}
}

// warmup draws the old-TV power-on: a bright line that opens up vertically.
func (e *boot) warmup(s *Screen, th *Theme) {
	cy := s.H / 2
	p := e.t / crtWarm
	if p < 0.5 {
		w := int(float64(s.W) * easeOut(p*2))
		for x := (s.W - w) / 2; x < (s.W+w)/2; x++ {
			s.Set(x, cy, '━', th.RainHot, true)
		}
		return
	}
	h := int(float64(s.H) / 2 * easeOut((p-0.5)*2))
	for y := cy - h; y <= cy+h; y++ {
		for x := 0; x < s.W; x++ {
			s.SetBG(x, y, lerp(th.BG, th.FG, 0.25*(1-(p-0.5)*2)))
		}
	}
}

// drawSwirl paints a rotating orange spiral that grows, then fades.
func (e *boot) drawSwirl(s *Screen, th *Theme, t float64) {
	orange := th.Syntax[KKeyword]
	cx, cy := float64(s.W)/2, float64(s.H)/2-2
	grow := easeOut(t / 1.2)
	fadeOut := 1 - clamp01((t-(swirlDur-0.5))/0.5)
	maxR := math.Min(float64(s.H)*0.42, float64(s.W)*0.2) * grow
	for y := 0; y < s.H; y++ {
		for x := 0; x < s.W; x++ {
			dx, dy := (float64(x)-cx)/2.1, float64(y)-cy
			r := math.Hypot(dx, dy)
			if r > maxR || r < 0.4 {
				continue
			}
			a := math.Atan2(dy, dx) - math.Log(r+1)*2.4 + t*3
			band := math.Mod(a+20*math.Pi, 2*math.Pi)
			if band < 2.2 {
				shade := (1 - band/2.2) * (1 - r/(maxR+1)*0.4) * fadeOut
				s.SetBG(x, y, lerp(th.BG, orange, shade))
			}
		}
	}
	if t > 1.3 {
		title := "B I L L B O A R D"
		a := clamp01((t-1.3)/0.5) * fadeOut
		s.Text(int(cx)-len(title)/2, int(cy+maxR)+2, title, lerp(th.BG, th.Syntax[KType], a), true)
	}
}

// ---- diff: replays a real commit, removed lines go red and collapse, new ones type in ----

type diffFx struct {
	cd    *CommitDiff
	l     Layout
	rng   *rand.Rand
	delay float64

	hunk, cur int
	st        int
	wait, ht  float64
	done      bool

	markedAt, doneAt []float64
	hidden, shown    []bool
	typed            []int
}

const (
	dIntro = iota
	dScan
	dCollapse
	dTyping
	dEnd
)

var (
	diffRed   = hex("#ff4d5e")
	diffGreen = hex("#3ddc84")
)

func buildDiff(f SourceFile, l Layout, c *Config, rng *rand.Rand) (Effect, *Doc, bool) {
	cd, ok := loadDiff(f, l.H-1, rng)
	if !ok {
		return nil, nil, false
	}
	subj := cd.Subject
	if len(subj) > 60 {
		subj = subj[:57] + "..."
	}
	d := &Doc{Path: f.Rel, Info: fmt.Sprintf("%s  %s  (%s)", cd.Hash, subj, cd.When)}
	e := &diffFx{cd: cd, l: l, rng: rng, delay: 1 / (c.CPS * 1.4)}
	e.reset()
	return e, d, true
}

func (e *diffFx) reset() {
	n := len(e.cd.Hunks[e.hunk])
	e.markedAt, e.doneAt = make([]float64, n), make([]float64, n)
	for i := range e.markedAt {
		e.markedAt[i], e.doneAt[i] = -1, -1
	}
	e.hidden, e.shown, e.typed = make([]bool, n), make([]bool, n), make([]int, n)
	e.cur, e.ht, e.st, e.wait = 0, 0, dIntro, 0.9
}

func (e *diffFx) Done() bool { return e.done }

func (e *diffFx) Update(dt float64) {
	e.ht += dt
	e.wait -= dt
	for e.wait <= 0 && !e.done {
		e.step()
	}
}

func (e *diffFx) step() {
	h := e.cd.Hunks[e.hunk]
	switch e.st {
	case dIntro:
		e.st = dScan
	case dScan:
		for e.cur < len(h) && h[e.cur].Kind == ' ' {
			e.cur++
		}
		switch {
		case e.cur >= len(h):
			e.st, e.wait = dEnd, e.wait+2.5
		case h[e.cur].Kind == '-':
			for j := e.cur; j < len(h) && h[j].Kind == '-'; j++ {
				e.markedAt[j] = e.ht
			}
			e.st, e.wait = dCollapse, e.wait+1.0
		default:
			e.shown[e.cur] = true
			e.st, e.wait = dTyping, e.wait+e.delay*4
		}
	case dCollapse:
		if e.cur < len(h) && h[e.cur].Kind == '-' {
			e.hidden[e.cur] = true
			e.cur++
			e.wait += 0.09
			return
		}
		e.st, e.wait = dScan, e.wait+0.25
	case dTyping:
		g := h[e.cur].G
		vis := min(len(g), e.l.W)
		if e.typed[e.cur] == 0 {
			for e.typed[e.cur] < vis && g[e.typed[e.cur]].Ch == ' ' {
				e.typed[e.cur]++
			}
		}
		if e.typed[e.cur] < vis {
			e.typed[e.cur]++
			e.wait += e.delay * (0.4 + e.rng.Float64()*1.2)
			return
		}
		e.doneAt[e.cur] = e.ht
		e.cur++
		if e.cur < len(h) && h[e.cur].Kind == '+' {
			e.shown[e.cur] = true
			e.wait += e.delay * 5
			return
		}
		e.st, e.wait = dScan, e.wait+0.3
	case dEnd:
		if e.hunk+1 >= len(e.cd.Hunks) {
			e.done = true // stay on the last hunk while the runner holds it
			return
		}
		e.hunk++
		e.reset()
	}
}

func (e *diffFx) Draw(s *Screen, th *Theme) {
	h := e.cd.Hunks[e.hunk]
	var vis []int
	active := 0
	for i, r := range h {
		if (r.Kind == '-' && e.hidden[i]) || (r.Kind == '+' && !e.shown[i]) {
			continue
		}
		if i <= e.cur {
			active = len(vis)
		}
		vis = append(vis, i)
	}
	// number rows by what's on screen right now, so the gutter stays sequential while
	// lines are removed and added; rows marked for deletion keep their old numbers
	nums := make([]int, len(vis))
	next := h[0].No
	for j, i := range vis {
		if h[i].Kind == '-' && e.markedAt[i] >= 0 {
			nums[j] = h[i].No
			continue
		}
		nums[j] = next
		next++
	}
	top := max(0, active-e.l.H+3)
	fadeIn := easeOut(e.ht / 0.5)
	if e.hunk == len(e.cd.Hunks)-1 && e.st == dEnd {
		fadeIn = 1
	}
	for row := 0; row < e.l.H && top+row < len(vis); row++ {
		i := vis[top+row]
		r := h[i]
		y := e.l.Y + row
		n := min(len(r.G), e.l.W)
		tint, mark := 0.0, ' '
		var tintCol RGB
		switch r.Kind {
		case '-':
			if e.markedAt[i] >= 0 {
				tint, mark, tintCol = 0.3*easeOut((e.ht-e.markedAt[i])/0.3), '-', diffRed
			}
		case '+':
			n = min(n, e.typed[i])
			mark, tintCol = '+', diffGreen
			tint = 0.22
			if e.doneAt[i] >= 0 { // fade the highlight out once the line is written
				tint = 0.06 + 0.16*(1-clamp01((e.ht-e.doneAt[i])/2.5))
			}
		}
		if tint > 0 {
			bg := lerp(th.BG, tintCol, tint)
			for x := e.l.X - 2; x < e.l.X+e.l.W; x++ {
				s.SetBG(x, y, bg)
			}
		}
		if e.l.Gutter > 0 { // one column narrower than usual to leave room for the +/- marker
			s.Text(e.l.X-e.l.Gutter, y, fmt.Sprintf("%*d", e.l.Gutter-3, nums[top+row]), lerp(th.BG, th.LineNr, fadeIn), false)
		}
		if mark != ' ' {
			s.Set(e.l.X-2, y, mark, tintCol, true)
		}
		for c := 0; c < n; c++ {
			col := th.Syntax[r.G[c].Kind]
			if r.Kind == '-' && e.markedAt[i] >= 0 {
				col = lerp(col, diffRed, 0.45)
			}
			s.Set(e.l.X+c, y, r.G[c].Ch, lerp(th.BG, col, fadeIn), false)
		}
		if r.Kind == '+' && i == e.cur && e.st == dTyping {
			s.Set(e.l.X+min(n, e.l.W-1), y, '▌', th.Cursor, false)
		}
	}
}
