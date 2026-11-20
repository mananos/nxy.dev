package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mananos/nxy.dev/v2/internal/app"
	"github.com/mananos/nxy.dev/v2/internal/format"
	"github.com/mananos/nxy.dev/v2/internal/gate"
	"github.com/mananos/nxy.dev/v2/internal/host/claudecode"
	"github.com/mananos/nxy.dev/v2/internal/platform"
	"github.com/mananos/nxy.dev/v2/internal/report"
)

// runGate is `nxy gate [status|once [search]|reads on|off]` (entries/gate.mjs of the 1.x): once arms
// a one-shot escape, reads on|off [--repo] writes gate.reads in the user config (or the repo one with --repo;
// a broken file is refused, not replaced), and every call ends with the status: the two gates, the escapes and what the ledger says.
func runGate(args []string, _ io.Reader, stdout, _ io.Writer) (code int) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(stdout, "error: %v\n", r)
			code = 1
		}
	}()
	repo, args := splitRepo(args)
	fa := format.ParseArgs(args)
	cwd := osGetwd()
	if v, ok := fa.Str("cwd"); ok {
		cwd = v
	} else if v := os.Getenv("CLAUDE_PROJECT_DIR"); v != "" {
		cwd = v
	}
	cwd = platform.NativePath(cwd)
	pos := fa.Positional
	action := "status"
	if len(pos) > 0 {
		action = strings.ToLower(pos[0])
	}
	sub := ""
	if len(pos) > 1 {
		sub = strings.ToLower(pos[1])
	}
	g := gateFiles{getenv: os.Getenv}
	mins := g.Settings(cwd).EscapeMinutes

	switch {
	case action == "once" && sub == "search":
		until, err := g.Arm(cwd, app.GateEscapeSearch)
		if err != nil {
			fmt.Fprintf(stdout, "error: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "gate: la próxima búsqueda amplia del hilo principal pasa sin frenarse (vence en %d min, %s).\n", mins, time.UnixMilli(until).Format("15:04:05"))
		fmt.Fprintln(stdout, "Se usa una sola vez: la búsqueda siguiente vuelve a pasar por el gate. Las ediciones no cambian.")
	case action == "once":
		until, err := g.Arm(cwd, app.GateEscapeEdit)
		if err != nil {
			fmt.Fprintf(stdout, "error: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "gate: la próxima edición de un archivo de código del hilo principal pasa sin bloquearse (vence en %d min, %s).\n", mins, time.UnixMilli(until).Format("15:04:05"))
		fmt.Fprintln(stdout, "Se usa una sola vez: la edición siguiente vuelve a pasar por el gate.")
	case action == "reads" && (sub == "on" || sub == "off"):
		userPath, repoPath, err := configFilePaths(cwd)
		if err != nil {
			fmt.Fprintf(stdout, "error: %v\n", err)
			return 1
		}
		p := userPath
		if repo {
			p = repoPath
		}
		doc, _, err := platform.ReadJSONDoc(p)
		if err != nil || doc == nil {
			fmt.Fprintf(stdout, "%s está roto; no se toca\n", filepath.ToSlash(p))
			return 1
		}
		if err := doc.Set([]string{"gate", "reads"}, sub == "on"); err != nil {
			fmt.Fprintf(stdout, "error: %v\n", err)
			return 1
		}
		if _, err := platform.WriteJSONDoc(p, doc); err != nil {
			fmt.Fprintf(stdout, "error: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "gate reads: %s (escrito en %s)\n", strings.ToUpper(sub), p)
		if sub == "on" {
			fmt.Fprintln(stdout, "Las búsquedas amplias del hilo principal vuelven a frenarse pasado el umbral.")
		} else {
			fmt.Fprintln(stdout, "Las búsquedas amplias sólo se observan: no se frena nada.")
		}
	case action != "status":
		fmt.Fprintf(stdout, "no conozco la acción %q. Usá: status | once [search] | reads on|off [--repo]\n", action)
	}

	now := time.Now()
	cfg := g.Settings(cwd)
	for _, l := range gate.StatusHead(gate.Status{
		Cfg:               cfg,
		EscapeUntil:       g.Until(cwd, app.GateEscapeEdit),
		SearchEscapeUntil: g.Until(cwd, app.GateEscapeSearch),
		NowMs:             now.UnixMilli(),
	}) {
		fmt.Fprintln(stdout, l)
	}
	fmt.Fprintln(stdout)
	for _, l := range gate.Summary(g.Rows(cwd)) {
		fmt.Fprintln(stdout, l)
	}

	// Broad searches the main thread ran past the threshold (observation only, never blocked).
	since := now.UnixMilli() - 30*86_400_000
	if v, ok := fa.Str("since"); ok {
		if ms, ok := format.ParseSince(v, now.UnixMilli(), time.Local); ok {
			since = ms
		}
	}
	date := time.UnixMilli(since).UTC().Format("2006-01-02")
	settings := report.SettingsFrom(platform.LoadConfig(cwd, os.Getenv))
	b := claudecode.ProjectBroadSearches(cwd, settings.ProjectsDir, since, settings.GateContextTokens)
	if b.Count == 0 {
		fmt.Fprintf(stdout, "búsquedas amplias:  ninguna registrada (desde %s)\n", date)
		return 0
	}
	order := []string{"Grep", "Glob", "Bash", "Read"}
	var kinds []string
	for _, t := range order {
		if n := b.ByKind.Get(t); n != 0 {
			kinds = append(kinds, t+" "+strconv.FormatFloat(n, 'f', -1, 64))
		}
	}
	for _, t := range b.ByKind.Keys() {
		if t != "Grep" && t != "Glob" && t != "Bash" && t != "Read" {
			kinds = append(kinds, t+" "+strconv.FormatFloat(b.ByKind.Get(t), 'f', -1, 64))
		}
	}
	thr := "?"
	if b.Threshold != nil && *b.Threshold != 0 {
		thr = format.FmtTokens(*b.Threshold)
	}
	fmt.Fprintf(stdout, "búsquedas amplias con más de %s de contexto (desde %s): %d — %s · contexto que cargaban esas búsquedas: %s tokens (suma del tamaño del contexto del hilo principal: un tope de lo que costó cada llamada, no de lo que habría ahorrado un scout)\n",
		thr, date, b.Count, strings.Join(kinds, " · "), format.FmtTokens(b.ContextTokens))
	return 0
}
