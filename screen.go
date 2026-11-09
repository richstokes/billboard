package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
)

type RGB struct{ R, G, B uint8 }

func hex(s string) RGB {
	if len(s) > 0 && s[0] == '#' {
		s = s[1:]
	}
	v, _ := strconv.ParseUint(s, 16, 32)
	return RGB{uint8(v >> 16), uint8(v >> 8), uint8(v)}
}

func lerp(a, b RGB, t float64) RGB {
	if t <= 0 {
		return a
	}
	if t >= 1 {
		return b
	}
	f := func(x, y uint8) uint8 { return uint8(float64(x) + (float64(y)-float64(x))*t + 0.5) }
	return RGB{f(a.R, b.R), f(a.G, b.G), f(a.B, b.B)}
}

type Cell struct {
	Ch   rune
	FG   RGB
	Bold bool
}

// Screen is a double-buffered cell grid that only writes changed cells.
type Screen struct {
	W, H        int
	cells, prev []Cell
	out         *bufio.Writer
	full        bool

	BG          RGB
	Transparent bool
	Brightness  float64
}

func NewScreen(w, h int) *Screen {
	s := &Screen{out: bufio.NewWriterSize(os.Stdout, 1<<16), Brightness: 1}
	s.Resize(w, h)
	return s
}

func (s *Screen) Resize(w, h int) {
	s.W, s.H = w, h
	s.cells = make([]Cell, w*h)
	s.prev = make([]Cell, w*h)
	s.full = true
}

func (s *Screen) Invalidate() { s.full = true }

func (s *Screen) Clear() {
	for i := range s.cells {
		s.cells[i] = Cell{Ch: ' ', FG: s.BG}
	}
}

func (s *Screen) Set(x, y int, ch rune, fg RGB, bold bool) {
	if x < 0 || y < 0 || x >= s.W || y >= s.H {
		return
	}
	s.cells[y*s.W+x] = Cell{ch, fg, bold}
}

func (s *Screen) Text(x, y int, str string, fg RGB, bold bool) {
	for _, r := range str {
		s.Set(x, y, r, fg, bold)
		x++
	}
}

func (s *Screen) Flush() {
	w := s.out
	w.WriteString("\x1b[?2026h") // synchronized update, ignored where unsupported
	bgSeq := "\x1b[49m"
	if !s.Transparent {
		bgSeq = fmt.Sprintf("\x1b[48;2;%d;%d;%dm", s.BG.R, s.BG.G, s.BG.B)
	}
	w.WriteString("\x1b[0m" + bgSeq)
	if s.full {
		w.WriteString("\x1b[2J")
	}
	var lastFG RGB
	lastBold, haveFG := false, false
	cx, cy := -1, -1
	for y := 0; y < s.H; y++ {
		for x := 0; x < s.W; x++ {
			i := y*s.W + x
			c := s.cells[i]
			if !s.full && c == s.prev[i] {
				continue
			}
			s.prev[i] = c
			if cx != x || cy != y {
				fmt.Fprintf(w, "\x1b[%d;%dH", y+1, x+1)
			}
			if c.Bold != lastBold || !haveFG {
				if c.Bold {
					w.WriteString("\x1b[1m")
				} else {
					w.WriteString("\x1b[22m")
				}
				lastBold = c.Bold
			}
			fg := lerp(s.BG, c.FG, s.Brightness)
			if fg != lastFG || !haveFG {
				fmt.Fprintf(w, "\x1b[38;2;%d;%d;%dm", fg.R, fg.G, fg.B)
				lastFG, haveFG = fg, true
			}
			w.WriteRune(c.Ch)
			cx, cy = x+1, y
		}
	}
	s.full = false
	w.WriteString("\x1b[?2026l")
	w.Flush()
}

func enterScreen() {
	os.Stdout.WriteString("\x1b[?1049h\x1b[?25l\x1b[2J")
}

func leaveScreen() {
	os.Stdout.WriteString("\x1b[0m\x1b[2J\x1b[?25h\x1b[?1049l")
}
