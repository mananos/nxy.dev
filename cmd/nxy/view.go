package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/mananos/nxy.dev/v2/internal/platform"
	"github.com/mananos/nxy.dev/v2/internal/store/sqlite"
	"github.com/mananos/nxy.dev/v2/internal/views"
)

const viewUsage = "uso: nxy view <cockpit|agents|decision|memory|stats|trend|config|plan|file|engineer> --json [--feature id] [--path ruta] [--scope repo|global] [--session id] [--cwd dir]"

// runView is `nxy view <id> --json`: it prints a view built from the real data of the repo in the
// shape of internal/views.
func runView(args []string, _ io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		fmt.Fprintln(stderr, viewUsage)
		return 1
	}
	id := args[0]
	if views.New(id) == nil {
		fmt.Fprintln(stderr, viewUsage)
		return 1
	}
	fs := flag.NewFlagSet("view", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print the view as JSON")
	feature := fs.String("feature", "", "the feature (agents, decision); default the active one of the folder")
	filePath := fs.String("path", "", "file: the file as written in the plan")
	scope := fs.String("scope", views.ScopeRepo, "memory: repo or global")
	session := fs.String("session", "", "stats: the session; default the current or the last one")
	cwd := fs.String("cwd", "", "the folder; default CLAUDE_PROJECT_DIR or the current one")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() > 0 || !*asJSON {
		fmt.Fprintln(stderr, viewUsage)
		return 1
	}
	if *scope != views.ScopeRepo && *scope != views.ScopeGlobal {
		fmt.Fprintln(stderr, viewUsage)
		return 1
	}
	if id == "file" && *filePath == "" {
		fmt.Fprintln(stderr, viewUsage)
		return 1
	}
	dir := resolveCwd(setupArgs{cwd: *cwd})

	path, err := platform.DBPath()
	if err != nil {
		fmt.Fprintf(stderr, "nxy view %s: %v\n", id, err)
		return 1
	}
	ctx := context.Background()
	st, err := sqlite.Open(ctx, path, nil)
	if err != nil {
		fmt.Fprintf(stderr, "nxy view %s: %v\n", id, err)
		return 1
	}
	defer st.Close()
	src := newViewSource(st, os.Getenv, dir, time.Now, time.Local)
	defer src.Close()

	var v any
	switch id {
	case "cockpit":
		v, err = src.Cockpit(ctx)
	case "agents", "decision", "plan", "file", "engineer":
		fid := *feature
		if fid == "" {
			var ok bool
			if fid, ok, err = src.ActiveFeatureID(ctx); err == nil && !ok {
				fmt.Fprintf(stderr, "nxy view %s: no hay una feature activa en esta carpeta (usá --feature)\n", id)
				return 1
			}
		}
		if err != nil {
			break
		}
		switch id {
		case "agents":
			v, err = src.Agents(ctx, fid)
		case "file":
			v, err = src.File(ctx, fid, *filePath)
		case "decision":
			var d views.Decision
			var ok bool
			if d, ok, err = src.Decision(ctx, fid); err == nil && ok {
				v = d
			}
		case "plan":
			var p views.Plan
			var ok bool
			if p, ok, err = src.Plan(ctx, fid); err == nil && ok {
				v = p
			}
		case "engineer":
			var e views.Engineer
			var ok bool
			if e, ok, err = src.Engineer(ctx, fid); err == nil && ok {
				v = e
			}
		}
	case "memory":
		v, err = src.Memory(ctx, *scope)
	case "stats":
		v, err = src.Stats(ctx, *session)
	case "trend":
		v, err = src.Trend(ctx)
	case "config":
		v, err = src.Config(ctx)
	}
	if err != nil {
		fmt.Fprintf(stderr, "nxy view %s: %v\n", id, err)
		return 1
	}
	enc := json.NewEncoder(stdout)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fmt.Fprintf(stderr, "nxy view %s: %v\n", id, err)
		return 1
	}
	return 0
}
