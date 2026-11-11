package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	nxydev "github.com/mananos/nxy.dev"
	"github.com/mananos/nxy.dev/internal/platform"
	"github.com/mananos/nxy.dev/internal/tui"
)

// runTui opens the F5.1 prototype over the sample views. The real TUI is F5.3.
func runTui(args []string, _ io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("tui", flag.ContinueOnError)
	fs.SetOutput(stderr)
	demo := fs.Bool("demo", false, "open the prototype with sample data")
	dump := fs.String("dump", "", "print a screen and exit")
	feature := fs.String("feature", "", "what the screen shows: a feature id, a memory scope or a file (default: the one of the screen)")
	width := fs.Int("width", 120, "width of the dump in cells")
	plain := fs.Bool("plain", false, "no ANSI in the dump")
	theme := fs.String("theme", "dark", "dump theme: dark|light")
	icons := fs.String("icons", "auto", "auto|nerd|unicode")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if !*demo {
		fmt.Fprintln(stderr, "nxy tui: por ahora sólo --demo (la TUI real es F5.3)")
		return 1
	}
	var nerd bool
	switch *icons {
	case "auto":
		nerd = platform.DetectIcons(os.Getenv)
	case "nerd":
		nerd = true
	case "unicode":
	default:
		fmt.Fprintf(stderr, "nxy tui: --icons %q: use auto, nerd or unicode\n", *icons)
		return 1
	}
	if *theme != "dark" && *theme != "light" {
		fmt.Fprintf(stderr, "nxy tui: --theme %q: use dark or light\n", *theme)
		return 1
	}
	data, err := tui.LoadDemo(nxydev.DemoViews)
	if err != nil {
		fmt.Fprintf(stderr, "nxy tui: %v\n", err)
		return 1
	}
	o := tui.Options{Data: data, Width: *width, Dark: *theme == "dark", Nerd: nerd, Plain: *plain}
	if *dump == "" {
		if err := tui.Run(o); err != nil {
			fmt.Fprintf(stderr, "nxy tui: %v\n", err)
			return 1
		}
		return 0
	}
	var ids []string
	known := false
	for _, t := range tui.DumpTargets() {
		ids = append(ids, t.ID)
		if t.ID == *dump {
			known = true
		}
	}
	if !known {
		fmt.Fprintf(stderr, "nxy tui: destino %q desconocido; válidos: %s\n", *dump, strings.Join(ids, ", "))
		return 1
	}
	out, err := tui.Render(o, *dump, *feature)
	if err != nil {
		fmt.Fprintf(stderr, "nxy tui: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, strings.TrimRight(out, "\n"))
	return 0
}
