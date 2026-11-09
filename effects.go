package main

import (
	"fmt"
	"math"
	"math/rand"
	"unicode"
)

type Layout struct {
	X, Y, W, H int // code area
	Gutter     int // line-number gutter width, drawn left of X (0 = off)
}

type Effect interface {
	Update(dt float64)
	Draw(s *Screen, th *Theme)
	Done() bool
}

type EffectSpec struct {
	Name    string
	Desc    string
	NoOutro bool                          // effect clears itself, skip hold + fade out
	Lines   func(l Layout, c *Config) int // how many lines of a file to show
	New     func(d *Doc, l Layout, c *Config, rng *rand.Rand) Effect
}

var effects = []EffectSpec{
	{Name: "typewriter", Desc: "types the code out live, with a cursor and the odd typo",
		Lines: func(l Layout, c *Config) int { return c.MaxLines },
		New:   newTypewriter},
	{Name: "scroll", Desc: "smooth credit-roll scroll through the file", NoOutro: true,
		Lines: func(l Layout, c *Config) int { return c.MaxLines * 3 },
		New:   newScroll},
	{Name: "matrix", Desc: "digital rain that reveals the code where it falls",
		Lines: func(l Layout, c *Config) int { return l.H },
		New:   newMatrix},
	{Name: "decrypt", Desc: "scrambled glyphs that resolve into code, line by line",
		Lines: func(l Layout, c *Config) int { return l.H },
		New:   newDecrypt},
	{Name: "fade", Desc: "lines gently fade and drift in (the subtle one)",
		Lines: func(l Layout, c *Config) int { return l.H },
		New:   newFade},
}

func effectByName(n string) int {
	for i, e := range effects {
		if e.Name == n {
			return i
		}
	}
	return -1
}

// ---- shared helpers ----

func hash(a, b, c int) uint32 {
	h := uint32(a)*0x9E3779B1 ^ uint32(b)*0x85EBCA77 ^ uint32(c)*0xC2B2AE3D
	h ^= h >> 15
	h *= 0x2C1B3C6D
	h ^= h >> 12
	return h
}

func hash01(a, b, c int) float64 { return float64(hash(a, b, c)%10000) / 10000 }

var katakana = []rune("ｱｲｳｴｵｶｷｸｹｺｻｼｽｾｿﾀﾁﾂﾃﾄﾅﾆﾇﾈﾉﾊﾋﾌﾍﾎﾏﾐﾑﾒﾓﾔﾕﾖﾗﾘﾙﾚﾛﾜﾝ0123456789")

// glyphPool mixes characters from the doc with katakana for rain/scramble.
func glyphPool(d *Doc) []rune {
	seen := map[rune]bool{}
	pool := append([]rune{}, katakana...)
	for _, l := range d.Lines {
		for _, g := range l {
			if !unicode.IsSpace(g.Ch) && !seen[g.Ch] {
				seen[g.Ch] = true
				pool = append(pool, g.Ch)
			}
		}
	}
	return pool
}

func drawGutter(s *Screen, th *Theme, l Layout, row, lineNo int) {
	drawGutterC(s, l, row, lineNo, th.LineNr)
}

func drawGutterC(s *Screen, l Layout, row, lineNo int, col RGB) {
	if l.Gutter == 0 {
		return
	}
	s.Text(l.X-l.Gutter, l.Y+row, fmt.Sprintf("%*d", l.Gutter-2, lineNo), col, false)
}

func glyphAt(d *Doc, row, col int) (Glyph, bool) {
	if row < 0 || row >= len(d.Lines) || col < 0 || col >= len(d.Lines[row]) {
		return Glyph{}, false
	}
	return d.Lines[row][col], true
}

func clamp01(x float64) float64 { return math.Max(0, math.Min(1, x)) }

// ---- typewriter ----

type typewriter struct {
	d         *Doc
	l         Layout
	rng       *rand.Rand
	delay     float64
	typos     bool
	line, col int
	wait, t   float64
	typo      rune
	lastTyped float64
}

func newTypewriter(d *Doc, l Layout, c *Config, rng *rand.Rand) Effect {
	e := &typewriter{d: d, l: l, rng: rng, delay: 1 / c.CPS, typos: c.Typos}
	e.indent()
	return e
}

func (e *typewriter) Done() bool { return e.line >= len(e.d.Lines) }

// indent jumps the cursor past leading whitespace, like an editor's auto-indent.
func (e *typewriter) indent() {
	if e.Done() {
		return
	}
	ln := e.d.Lines[e.line]
	for e.col < min(len(ln), e.l.W) && ln[e.col].Ch == ' ' {
		e.col++
	}
}

func (e *typewriter) Update(dt float64) {
	e.t += dt
	e.wait -= dt
	for e.wait <= 0 && !e.Done() {
		e.step()
		e.lastTyped = e.t
	}
}

func (e *typewriter) step() {
	ln := e.d.Lines[e.line]
	vis := min(len(ln), e.l.W)
	jit := func(lo, hi float64) float64 { return e.delay * (lo + e.rng.Float64()*(hi-lo)) }
	if e.typo != 0 { // backspace the mistake
		e.typo = 0
		e.wait += jit(1.5, 3)
		return
	}
	if e.col >= vis {
		e.line++
		e.col = 0
		e.indent()
		if vis == 0 {
			e.wait += jit(1, 3)
		} else {
			e.wait += jit(3, 9)
		}
		return
	}
	ch := ln[e.col].Ch
	if e.typos && unicode.IsLetter(ch) && e.rng.Float64() < 0.012 {
		e.typo = rune('a' + e.rng.Intn(26))
		e.wait += jit(5, 12)
		return
	}
	e.col++
	switch {
	case ch == ' ' && e.rng.Float64() < 0.15:
		e.wait += jit(2, 5)
	case ch == '(' || ch == '{' || ch == ',' || ch == '.':
		e.wait += jit(1, 2.5)
	default:
		e.wait += jit(0.4, 1.6)
	}
}

func (e *typewriter) Draw(s *Screen, th *Theme) {
	n := len(e.d.Lines)
	curRow, curCol := e.line, e.col
	if e.Done() {
		curRow, curCol = n-1, min(len(e.d.Lines[n-1]), e.l.W)
	}
	top := max(0, curRow-e.l.H+3)
	for row := 0; row < e.l.H && top+row <= curRow; row++ {
		i := top + row
		drawGutter(s, th, e.l, row, e.d.FirstLine+i)
		ln := e.d.Lines[i]
		k := min(len(ln), e.l.W)
		if i == curRow {
			k = min(k, curCol)
		}
		for c := 0; c < k; c++ {
			s.Set(e.l.X+c, e.l.Y+row, ln[c].Ch, th.Syntax[ln[c].Kind], false)
		}
	}
	row, col := curRow-top, curCol
	if e.typo != 0 {
		s.Set(e.l.X+col, e.l.Y+row, e.typo, th.FG, false)
		col++
	}
	if e.t-e.lastTyped < 0.4 || math.Mod(e.t, 1.06) < 0.53 {
		s.Set(e.l.X+min(col, e.l.W-1), e.l.Y+row, '▌', th.Cursor, false)
	}
}

// ---- scroll ----

type scroller struct {
	d   *Doc
	l   Layout
	pos float64
	lps float64
}

func newScroll(d *Doc, l Layout, c *Config, rng *rand.Rand) Effect {
	return &scroller{d: d, l: l, pos: -float64(l.H - 1), lps: c.LPS}
}

func (e *scroller) Update(dt float64) { e.pos += dt * e.lps }
func (e *scroller) Done() bool        { return e.pos >= float64(len(e.d.Lines)) }

func (e *scroller) Draw(s *Screen, th *Theme) {
	top := int(math.Floor(e.pos))
	for row := 0; row < e.l.H; row++ {
		i := top + row
		if i < 0 || i >= len(e.d.Lines) {
			continue
		}
		// soften the top and bottom edges so lines ease in and out
		edge := clamp01(math.Min(float64(row)+1, float64(e.l.H-row)) / 4)
		drawGutter(s, th, e.l, row, e.d.FirstLine+i)
		ln := e.d.Lines[i]
		for c := 0; c < min(len(ln), e.l.W); c++ {
			s.Set(e.l.X+c, e.l.Y+row, ln[c].Ch, lerp(th.BG, th.Syntax[ln[c].Kind], edge), false)
		}
	}
}

// ---- matrix ----

type drop struct {
	x      int
	y, v   float64
	length int
}

type matrix struct {
	d         *Doc
	l         Layout
	rng       *rand.Rand
	t         float64
	revealAt  []float64
	remaining int
	lastRev   float64
	drops     []drop
	pool      []rune
}

func newMatrix(d *Doc, l Layout, c *Config, rng *rand.Rand) Effect {
	e := &matrix{d: d, l: l, rng: rng, revealAt: make([]float64, l.W*l.H), pool: glyphPool(d)}
	for i := range e.revealAt {
		e.revealAt[i] = -1
		if g, ok := glyphAt(d, i/l.W, i%l.W); ok && g.Ch != ' ' {
			e.remaining++
		}
	}
	return e
}

func (e *matrix) reveal(x, y int) {
	if x < 0 || y < 0 || x >= e.l.W || y >= e.l.H {
		return
	}
	i := y*e.l.W + x
	if e.revealAt[i] >= 0 {
		return
	}
	e.revealAt[i] = e.t
	if g, ok := glyphAt(e.d, y, x); ok && g.Ch != ' ' {
		e.remaining--
		e.lastRev = e.t
	}
}

func (e *matrix) Update(dt float64) {
	e.t += dt
	for x := 0; x < e.l.W; x++ {
		if e.rng.Float64() < 0.22*dt {
			e.drops = append(e.drops, drop{
				x: x, y: -e.rng.Float64() * 4, v: 12 + e.rng.Float64()*22,
				length: 5 + e.rng.Intn(max(6, e.l.H*2/3)),
			})
		}
	}
	live := e.drops[:0]
	for _, d := range e.drops {
		from := int(d.y)
		d.y += d.v * dt
		for y := max(0, from); y <= int(d.y); y++ {
			e.reveal(d.x, y)
		}
		if int(d.y)-d.length < e.l.H {
			live = append(live, d)
		}
	}
	e.drops = live
	if e.t > 20 && e.remaining > 0 { // stragglers: just finish it off
		for i := range e.revealAt {
			e.reveal(i%e.l.W, i/e.l.W)
		}
	}
}

func (e *matrix) Done() bool { return e.remaining <= 0 && e.t-e.lastRev > 0.8 }

func (e *matrix) Draw(s *Screen, th *Theme) {
	for y := 0; y < e.l.H; y++ {
		shown := false
		for x := 0; x < e.l.W; x++ {
			at := e.revealAt[y*e.l.W+x]
			g, ok := glyphAt(e.d, y, x)
			if at < 0 || !ok || g.Ch == ' ' {
				continue
			}
			shown = true
			a := clamp01((e.t - at) / 0.9)
			s.Set(e.l.X+x, e.l.Y+y, g.Ch, lerp(th.RainHot, th.Syntax[g.Kind], a), false)
		}
		if shown {
			drawGutter(s, th, e.l, y, e.d.FirstLine+y)
		}
	}
	for di, d := range e.drops {
		head := int(d.y)
		for k := 0; k < d.length; k++ {
			y := head - k
			if y < 0 || y >= e.l.H {
				continue
			}
			fade := 1 - float64(k)/float64(d.length)
			if g, ok := glyphAt(e.d, y, d.x); ok && g.Ch != ' ' && fade < 0.6 {
				continue // let revealed code win over the tail
			}
			ch := e.pool[hash(d.x, y, int(e.t*9)+di)%uint32(len(e.pool))]
			col := lerp(th.BG, th.Rain, fade*0.85)
			if k == 0 {
				col = th.RainHot
			}
			s.Set(e.l.X+d.x, e.l.Y+y, ch, col, k == 0)
		}
	}
}

// ---- decrypt ----

type decrypt struct {
	d    *Doc
	l    Layout
	t    float64
	end  float64
	pool []rune
	seed int
}

const (
	decLineDelay = 0.07
	decCharDelay = 0.01
)

func newDecrypt(d *Doc, l Layout, c *Config, rng *rand.Rand) Effect {
	e := &decrypt{d: d, l: l, pool: glyphPool(d), seed: rng.Int()}
	for row, ln := range d.Lines {
		e.end = math.Max(e.end, e.resolve(row, min(len(ln), l.W)))
	}
	return e
}

func (e *decrypt) start(row, col int) float64 {
	return float64(row)*decLineDelay + float64(col)*decCharDelay*0.35
}

func (e *decrypt) resolve(row, col int) float64 {
	return e.start(row, col) + 0.25 + float64(col)*decCharDelay + hash01(row, col, e.seed)*0.6
}

func (e *decrypt) Update(dt float64) { e.t += dt }
func (e *decrypt) Done() bool        { return e.t > e.end+0.3 }

func (e *decrypt) Draw(s *Screen, th *Theme) {
	scr := lerp(th.BG, th.Rain, 0.75)
	for row := 0; row < min(len(e.d.Lines), e.l.H); row++ {
		if e.t < e.start(row, 0) {
			break
		}
		drawGutter(s, th, e.l, row, e.d.FirstLine+row)
		ln := e.d.Lines[row]
		for c := 0; c < min(len(ln), e.l.W); c++ {
			g := ln[c]
			if g.Ch == ' ' || e.t < e.start(row, c) {
				continue
			}
			r := e.resolve(row, c)
			if e.t < r {
				ch := e.pool[hash(row, c, int(e.t*18)+e.seed)%uint32(len(e.pool))]
				s.Set(e.l.X+c, e.l.Y+row, ch, scr, false)
				continue
			}
			s.Set(e.l.X+c, e.l.Y+row, g.Ch, lerp(th.RainHot, th.Syntax[g.Kind], (e.t-r)/0.25), false)
		}
	}
}

// ---- fade ----

type fade struct {
	d *Doc
	l Layout
	t float64
}

const (
	fadeLineDelay = 0.11
	fadeDur       = 0.9
)

func newFade(d *Doc, l Layout, c *Config, rng *rand.Rand) Effect { return &fade{d: d, l: l} }

func (e *fade) Update(dt float64) { e.t += dt }
func (e *fade) Done() bool {
	return e.t > float64(len(e.d.Lines)-1)*fadeLineDelay+fadeDur
}

func (e *fade) Draw(s *Screen, th *Theme) {
	for row := 0; row < min(len(e.d.Lines), e.l.H); row++ {
		a := clamp01((e.t - float64(row)*fadeLineDelay) / fadeDur)
		if a <= 0 {
			break
		}
		ease := 1 - math.Pow(1-a, 3)
		shift := int(math.Round((1 - ease) * 6))
		ln := e.d.Lines[row]
		drawGutterC(s, e.l, row, e.d.FirstLine+row, lerp(th.BG, th.LineNr, ease))
		for c := 0; c < min(len(ln), e.l.W-shift); c++ {
			s.Set(e.l.X+c+shift, e.l.Y+row, ln[c].Ch, lerp(th.BG, th.Syntax[ln[c].Kind], ease), false)
		}
	}
}
