package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mananos/nxy.dev/v2/internal/configedit"
	"github.com/mananos/nxy.dev/v2/internal/filter"
	"github.com/mananos/nxy.dev/v2/internal/host/claudecode"
	"github.com/mananos/nxy.dev/v2/internal/platform"
	"github.com/mananos/nxy.dev/v2/internal/tools"
)

const configUsage = "uso: nxy config [show] | set <clave> <valor|default> [--repo] | cache-ttl 1h|off"

// runConfig is `nxy config [show] | set <key> <value|default> [--repo] | cache-ttl 1h|off`.
// set writes ~/.nxy/config.json (or <git root>/.nxy/config.json with --repo): a few hundred bytes of
// the user's, kept on disk until the user deletes it. cache-ttl edits ~/.claude/settings.json and
// keeps up to 3 backups next to it (`settings.json.bak-nxy-<ms>`, each the size of the settings).
func runConfig(args []string, _ io.Reader, stdout, _ io.Writer) (code int) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(stdout, "error: %v\n", r)
			code = 1
		}
	}()
	a := parseSetupArgs(args)
	cwd := resolveCwd(a)
	repo, pos := splitRepo(a.positional)
	action := "show"
	if len(pos) > 0 {
		action = strings.ToLower(pos[0])
	}
	switch action {
	case "show":
		return configShow(cwd, stdout)
	case "set":
		if len(pos) != 3 {
			fmt.Fprintln(stdout, configUsage)
			return 1
		}
		return configSet(cwd, pos[1], pos[2], repo, stdout)
	case "cache-ttl":
		arg := ""
		if len(pos) > 1 {
			arg = pos[1]
		}
		return configCacheTTL(arg, stdout)
	}
	fmt.Fprintln(stdout, configUsage)
	return 1
}

// fileState is "existe", "no existe" or "roto" for the config file at path.
func fileState(path string) (string, map[string]any) {
	doc, exists, err := platform.ReadJSONDoc(path)
	switch {
	case err != nil:
		return "roto", nil
	case !exists:
		return "no existe", nil
	}
	m, _ := doc.Get()
	obj, _ := m.(map[string]any)
	return "existe", obj
}

func displayValue(v any) string {
	switch x := v.(type) {
	case nil:
		return "-"
	case string:
		return x
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func sourceLabel(s configedit.Source) string {
	switch s {
	case configedit.SourceUser:
		return "usuario"
	case configedit.SourceRepo:
		return "repo"
	case configedit.SourceEnv:
		return "env"
	}
	return "predeterminado"
}

func lookupPath(cfg map[string]any, path []string) any {
	var cur any = cfg
	for _, k := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[k]
	}
	return cur
}

func configShow(cwd string, w io.Writer) int {
	userPath, repoPath, err := configFilePaths(cwd)
	if err != nil {
		fmt.Fprintf(w, "error: %v\n", err)
		return 1
	}
	userState, userObj := fileState(userPath)
	repoState, repoObj := fileState(repoPath)
	files := configedit.Files{User: userObj, Repo: repoObj}
	cfg := platform.LoadConfig(cwd, os.Getenv)

	fmt.Fprintln(w, "Config de nxy (valor efectivo y de dónde viene)")
	group := ""
	for _, f := range configedit.Fields() {
		g := "Filtro"
		if strings.HasPrefix(f.ID, "roles.") {
			g = "Roles (sin valor = lo que dice el agente)"
		}
		if g != group {
			fmt.Fprintf(w, "\n%s\n", g)
			group = g
		}
		fmt.Fprintf(w, "  %-32s %-8s (%s)\n", f.ID, displayValue(lookupPath(cfg, f.Path)), sourceLabel(configedit.SourcesOf(f.ID, files, os.Getenv)))
	}
	if bad := configedit.Invalid(cfg); len(bad) > 0 {
		fmt.Fprintln(w, "\nAvisos")
		for _, p := range bad {
			fmt.Fprintf(w, "  %s\n", p.Message())
		}
	}
	fmt.Fprintf(w, "\nArchivos\n  usuario: %s (%s)\n  repo:    %s (%s)\n", filepath.ToSlash(userPath), userState, filepath.ToSlash(repoPath), repoState)
	fmt.Fprintf(w, "\nCambiar: nxy config set <clave> <valor|default> [--repo]  (sin --repo guarda en el usuario)\n")
	return 0
}

func configSet(cwd, key, raw string, repo bool, w io.Writer) int {
	var value any
	if raw == "default" {
		value = nil
	} else if err := json.Unmarshal([]byte(raw), &value); err != nil {
		value = raw
	}
	if ok, reason := configedit.Validate(key, value); !ok {
		fmt.Fprintln(w, reason)
		return 1
	}
	userPath, repoPath, err := configFilePaths(cwd)
	if err != nil {
		fmt.Fprintf(w, "error: %v\n", err)
		return 1
	}
	path := userPath
	if repo {
		path = repoPath
	}
	doc, _, err := platform.ReadJSONDoc(path)
	if err != nil || doc == nil {
		fmt.Fprintf(w, "%s está roto; no se toca\n", filepath.ToSlash(path))
		return 1
	}
	field, _ := configedit.Lookup(key)
	if value == nil {
		doc.Delete(field.Path...)
	} else if err := doc.Set(field.Path, value); err != nil {
		fmt.Fprintf(w, "error: %v\n", err)
		return 1
	}
	n, err := platform.WriteJSONDoc(path, doc)
	if err != nil {
		fmt.Fprintf(w, "error: %v\n", err)
		return 1
	}
	if value == nil {
		fmt.Fprintf(w, "%s vuelve al predeterminado en %s\n", key, filepath.ToSlash(path))
	} else {
		fmt.Fprintf(w, "%s = %s en %s (%d bytes)\n", key, displayValue(value), filepath.ToSlash(path), n)
	}
	if key == "modules.filter" || key == "filter.engine" {
		s := platform.LoadFilterSettings(cwd, os.Getenv)
		tools.Engine{Getenv: os.Getenv}.Refresh(s, func() bool {
			return filter.RtkHookIn(claudecode.PreToolUseHookCommands(claudecode.SettingsFiles(homeDir(), cwd)))
		})
	}
	return 0
}

func configCacheTTL(arg string, w io.Writer) int {
	if arg != "1h" && arg != "off" {
		fmt.Fprintln(w, "uso: cache-ttl 1h|off")
		return 1
	}
	path := filepath.Join(homeDir(), ".claude", "settings.json")
	doc, exists, err := platform.ReadJSONDoc(path)
	if err != nil || doc == nil {
		fmt.Fprintf(w, "%s está roto; no se toca\n", filepath.ToSlash(path))
		return 1
	}
	if arg == "1h" {
		if err := doc.Set([]string{"promptCacheTtl"}, "1h"); err != nil {
			fmt.Fprintf(w, "error: %v\n", err)
			return 1
		}
	} else {
		doc.Delete("promptCacheTtl")
	}
	backup, kept := "", 0
	if exists {
		if backup, kept, err = platform.BackupAndPrune(path, time.Now()); err != nil {
			fmt.Fprintf(w, "error: %v\n", err)
			return 1
		}
	}
	if _, err := platform.WriteJSONDoc(path, doc); err != nil {
		fmt.Fprintf(w, "error: %v\n", err)
		return 1
	}
	if arg == "1h" {
		fmt.Fprintf(w, "promptCacheTtl = 1h en %s\n", filepath.ToSlash(path))
	} else {
		fmt.Fprintf(w, "promptCacheTtl quitado en %s\n", filepath.ToSlash(path))
	}
	if exists {
		fmt.Fprintf(w, "backup: %s (se conservan %d)\n", filepath.ToSlash(backup), kept)
	} else {
		fmt.Fprintln(w, "backup: ninguno (el archivo no existía)")
	}
	fmt.Fprintln(w, "Claude Code lo toma en la próxima sesión.")
	return 0
}
