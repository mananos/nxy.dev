package tui

// icons is one glyph set. The Unicode set uses only glyphs of safe width 1.
type icons struct {
	Brand, Working, Verifying, Pending, Done, Failed, NeedsYou string
	Arrow, Branch, Dirty, Up, Down                             string
	Top, Mid, Last, Vert, Horiz                                string
}

var unicodeIcons = icons{
	Brand: "◆", Working: "●", Verifying: "◐", Pending: "○", Done: "✓", Failed: "✗", NeedsYou: "●",
	Arrow: "▸", Branch: "", Dirty: "*", Up: "↑", Down: "↓",
	Top: "┌", Mid: "├", Last: "└", Vert: "│", Horiz: "─",
}

// Nerd Font private-use glyphs (Font Awesome range).
var nerdIcons = icons{
	Brand: "", Working: "", Verifying: "", Pending: "", Done: "", Failed: "", NeedsYou: "",
	Arrow: "", Branch: " ", Dirty: "", Up: "", Down: "",
	Top: "┌", Mid: "├", Last: "└", Vert: "│", Horiz: "─",
}

func iconSet(nerd bool) icons {
	if nerd {
		return nerdIcons
	}
	return unicodeIcons
}
