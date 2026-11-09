package main

type Kind uint8

const (
	KText Kind = iota
	KKeyword
	KType
	KFunction
	KString
	KNumber
	KComment
	KOperator
	KPunct
	KBuiltin
	numKinds
)

type Theme struct {
	Name    string
	BG, FG  RGB
	Syntax  [numKinds]RGB
	LineNr  RGB
	Header  RGB
	Cursor  RGB
	Rain    RGB // matrix trail / scramble glyphs
	RainHot RGB // matrix drop head
}

// palette order: keyword, type, function, string, number, comment, operator, punct, builtin
func mk(name, bg, fg, lineNr, header, cursor, rain, rainHot string, syn [9]string) *Theme {
	t := &Theme{
		Name: name, BG: hex(bg), FG: hex(fg), LineNr: hex(lineNr), Header: hex(header),
		Cursor: hex(cursor), Rain: hex(rain), RainHot: hex(rainHot),
	}
	t.Syntax[KText] = t.FG
	for i, c := range syn {
		t.Syntax[Kind(i+1)] = hex(c)
	}
	return t
}

// mono builds a single-hue theme (CRT style) from dark to bright shades.
func mono(name, bg, dim, mid, fg, bright string) *Theme {
	return mk(name, bg, fg, dim, mid, bright, mid, bright,
		[9]string{bright, bright, fg, mid, mid, dim, mid, mid, bright})
}

var themes = []*Theme{
	mk("dracula", "#1e1f29", "#f8f8f2", "#44475a", "#6272a4", "#f8f8f2", "#50fa7b", "#d8ffe0",
		[9]string{"#ff79c6", "#8be9fd", "#50fa7b", "#f1fa8c", "#bd93f9", "#6272a4", "#ff79c6", "#d6d6d0", "#8be9fd"}),
	mk("tokyonight", "#16161e", "#c0caf5", "#3b4261", "#565f89", "#c0caf5", "#7aa2f7", "#e0e8ff",
		[9]string{"#bb9af7", "#2ac3de", "#7aa2f7", "#9ece6a", "#ff9e64", "#565f89", "#89ddff", "#a9b1d6", "#e0af68"}),
	mk("monokai", "#1c1d18", "#f8f8f2", "#49483e", "#75715e", "#f8f8f0", "#a6e22e", "#efffd0",
		[9]string{"#f92672", "#66d9ef", "#a6e22e", "#e6db74", "#ae81ff", "#75715e", "#f92672", "#cfcfc2", "#fd971f"}),
	mk("nord", "#242933", "#d8dee9", "#3b4252", "#616e88", "#eceff4", "#88c0d0", "#e5f6fb",
		[9]string{"#81a1c1", "#8fbcbb", "#88c0d0", "#a3be8c", "#b48ead", "#616e88", "#81a1c1", "#aab3c2", "#d08770"}),
	mk("gruvbox", "#1d2021", "#ebdbb2", "#3c3836", "#7c6f64", "#ebdbb2", "#b8bb26", "#f2f5c0",
		[9]string{"#fb4934", "#fabd2f", "#b8bb26", "#b8bb26", "#d3869b", "#928374", "#fe8019", "#d5c4a1", "#8ec07c"}),
	mk("synthwave", "#1a1028", "#f0e6ff", "#3d2a55", "#7a5c9a", "#ff7edb", "#ff7edb", "#ffe3f7",
		[9]string{"#fede5d", "#36f9f6", "#ff7edb", "#ff8b39", "#f97e72", "#7a5c9a", "#fede5d", "#c9b8e0", "#36f9f6"}),
	mk("github-dark", "#0d1117", "#e6edf3", "#30363d", "#7d8590", "#e6edf3", "#3fb950", "#d2ffd9",
		[9]string{"#ff7b72", "#ffa657", "#d2a8ff", "#a5d6ff", "#79c0ff", "#8b949e", "#ff7b72", "#c9d1d9", "#79c0ff"}),
	// Commodore 64, using colors from the Pepto palette: light blue on blue, white cursor-ish highlights
	mk("c64", "#352879", "#6c5eb5", "#5246a0", "#6c5eb5", "#6c5eb5", "#6c5eb5", "#ffffff",
		[9]string{"#ffffff", "#70a4b2", "#9ad284", "#b8c76f", "#9a6759", "#959595", "#6c5eb5", "#6c5eb5", "#70a4b2"}),
	// Late-90s Sega: swirl orange and blue on a deep navy, accents from the colored Saturn buttons
	mk("dreamcast", "#0a0f26", "#e6ebf5", "#262f5c", "#4a7fe0", "#ff6a13", "#2f8cff", "#e6f3ff",
		[9]string{"#ff6a13", "#3d9bff", "#7fd4ff", "#ffd23f", "#ff4d6d", "#4c5a8f", "#ff8f4d", "#9aa6c8", "#5fe08a"}),
	mono("matrix", "#000500", "#0a3d12", "#118a2c", "#3cff6e", "#c8ffd6"),
	mono("amber", "#0c0700", "#4a2c00", "#9c6100", "#ffb000", "#ffe0a0"),
	mono("ice", "#00080c", "#0b3a4a", "#1f7f99", "#5fd7ff", "#d8f6ff"),
	mono("mono", "#0b0b0b", "#333333", "#777777", "#bbbbbb", "#ffffff"),
}

func themeByName(n string) (*Theme, int) {
	for i, t := range themes {
		if t.Name == n {
			return t, i
		}
	}
	return nil, -1
}
