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

	"github.com/mananos/nxy.dev/internal/platform"
	"github.com/mananos/nxy.dev/internal/store/sqlite"
	"github.com/mananos/nxy.dev/internal/views"
)

const viewUsage = "usage: nxy view <cockpit|agents|decision|memory|stats|trend|config> --json [--feature id] [--scope repo|global] [--session id] [--cwd dir]"

// laterViews are the views that need more than the cheap reads; they arrive in F5.2c.
var laterViews = map[string]bool{"plan": true, "file": true, "engineer": true}

// runView is `nxy view <id> --json`: it prints a view built from the real data of the repo in the
// shape of internal/views.
func runView(args []string, _ io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		fmt.Fprintln(stderr, viewUsage)
		return 1
	}
	id := args[0]
	if laterViews[id] {
		fmt.Fprintf(stderr, "nxy view %s: llega en F5.2c\n", id)
		return 1
	}
	if views.New(id) == nil {
		fmt.Fprintln(stderr, viewUsage)
		return 1
	}
	fs := flag.NewFlagSet("view", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "print the view as JSON")
	feature := fs.String("feature", "", "the feature (agents, decision); default the active one of the folder")
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
	case "agents", "decision":
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
		if id == "agents" {
			v, err = src.Agents(ctx, fid)
			break
		}
		var d views.Decision
		var ok bool
		if d, ok, err = src.Decision(ctx, fid); err == nil {
			if ok {
				v = d
			} else {
				v = nil
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
