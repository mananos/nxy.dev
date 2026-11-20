package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	nxydev "github.com/mananos/nxy.dev/v2"
	"github.com/mananos/nxy.dev/v2/internal/platform"
)

// The statusline runs a stable copy of the shim, ~/.nxy/bin/nxy.cmd (about 13 KB), never the one
// inside the versioned plugin folder: that path changes with every update. The copy is the shim of
// the binary that wrote it (embedded), and every SessionStart puts it back in line with the running
// binary, so after an update the statusline follows the hooks. It is always there (SessionStart
// writes it) and is never removed: the PATH entry and /nxy:tui use it too.

type statuslineSetup struct {
	home    string // OS home: Claude Code's settings live in <home>/.claude
	userDir string // nxy's user dir (NXY_HOME or ~/.nxy)
	goos    string
	now     func() time.Time
}

func realStatuslineSetup() (statuslineSetup, error) {
	u, err := platform.UserDir()
	if err != nil {
		return statuslineSetup{}, err
	}
	return statuslineSetup{home: homeDir(), userDir: u, goos: runtime.GOOS, now: time.Now}, nil
}

func (s statuslineSetup) settingsPath() string {
	return filepath.Join(s.home, ".claude", "settings.json")
}

func stableShimPath(userDir string) string { return filepath.Join(userDir, "bin", "nxy.cmd") }

// command is the statusLine command: sourced on Windows (Git Bash or PowerShell, the polyglot
// line of the hooks), run as a script elsewhere (works under dash, whose `.` takes no arguments).
func (s statuslineSetup) command() string {
	p := filepath.ToSlash(stableShimPath(s.userDir))
	if s.goos == "windows" {
		return fmt.Sprintf(`. "%s" statusline; exit $LASTEXITCODE`, p)
	}
	return fmt.Sprintf(`sh "%s" statusline`, p)
}

// statusLineCfg keeps the key order of the 1.x snippet.
type statusLineCfg struct {
	Type            string `json:"type"`
	Command         string `json:"command"`
	Padding         int    `json:"padding"`
	RefreshInterval int    `json:"refreshInterval"`
}

func (s statuslineSetup) snippet() statusLineCfg {
	return statusLineCfg{Type: "command", Command: s.command(), Padding: 0, RefreshInterval: 30}
}

// isOurs: the command runs nxy's stable shim.
func (s statuslineSetup) isOurs(cmd any) bool {
	c, _ := cmd.(string)
	return c != "" && strings.Contains(filepath.ToSlash(c), filepath.ToSlash(stableShimPath(s.userDir)))
}

func oneLineJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func (s statuslineSetup) show(w io.Writer) int {
	snip := struct {
		StatusLine statusLineCfg `json:"statusLine"`
	}{s.snippet()}
	b, _ := json.MarshalIndent(snip, "", "  ")
	fmt.Fprintln(w, "Statusline de nxy: va en ~/.claude/settings.json (Claude Code no la toma de un plugin).")
	fmt.Fprintln(w, string(b))
	fmt.Fprintf(w, "\nEl comando corre una copia del shim en %s que sigue a la versión de nxy instalada; install la escribe.\n", filepath.ToSlash(stableShimPath(s.userDir)))
	doc, exists, err := platform.ReadJSONDoc(s.settingsPath())
	switch {
	case err != nil:
		fmt.Fprintf(w, "\n%s está roto: install no lo va a tocar hasta que lo arregles.\n", filepath.ToSlash(s.settingsPath()))
	case !exists:
		fmt.Fprintln(w, "\nNo hay settings.json todavía: install lo crea.")
	default:
		if cur, ok := doc.Get("statusLine"); ok {
			m, _ := cur.(map[string]any)
			if s.isOurs(m["command"]) {
				fmt.Fprintln(w, "\nYa está instalada.")
			} else {
				fmt.Fprintf(w, "\nstatusLine actual: %s (install la reemplaza; queda un backup)\n", oneLineJSON(cur))
			}
		} else {
			fmt.Fprintln(w, "\nNo hay statusLine configurada.")
		}
	}
	fmt.Fprintln(w, "Para instalarla: /nxy:statusline install · para quitarla: /nxy:statusline remove · reiniciá Claude Code después.")
	return 0
}

func (s statuslineSetup) install(w io.Writer) int {
	path := s.settingsPath()
	doc, exists, err := platform.ReadJSONDoc(path)
	if err != nil {
		fmt.Fprintf(w, "%s está roto (%v); no se toca.\n", filepath.ToSlash(path), err)
		return 1
	}
	shim := stableShimPath(s.userDir)
	if err := os.MkdirAll(filepath.Dir(shim), 0o755); err != nil {
		fmt.Fprintf(w, "no pude crear %s: %v\n", filepath.ToSlash(filepath.Dir(shim)), err)
		return 1
	}
	if err := platform.WriteAtomic(shim, nxydev.Shim); err != nil {
		fmt.Fprintf(w, "no pude escribir el shim: %v\n", err)
		return 1
	}
	backup := ""
	if exists {
		b, kept, err := platform.BackupAndPrune(path, s.now())
		if err != nil {
			fmt.Fprintf(w, "no pude hacer el backup de %s: %v; no se toca.\n", filepath.ToSlash(path), err)
			return 1
		}
		backup = fmt.Sprintf("%s (se conservan %d)", filepath.ToSlash(b), kept)
	}
	if err := doc.Set([]string{"statusLine"}, s.snippet()); err != nil {
		fmt.Fprintf(w, "error: %v\n", err)
		return 1
	}
	if _, err := platform.WriteJSONDoc(path, doc); err != nil {
		fmt.Fprintf(w, "no pude escribir %s: %v\n", filepath.ToSlash(path), err)
		return 1
	}
	fmt.Fprintf(w, "shim: %s (%d KB)\n", filepath.ToSlash(shim), (len(nxydev.Shim)+512)/1024)
	if backup != "" {
		fmt.Fprintf(w, "backup: %s\n", backup)
	} else {
		fmt.Fprintln(w, "backup: ninguno (el archivo no existía)")
	}
	fmt.Fprintf(w, "statusLine escrita en %s. Reiniciá Claude Code para verla.\n", filepath.ToSlash(path))
	return 0
}

func (s statuslineSetup) remove(w io.Writer) int {
	path := s.settingsPath()
	doc, exists, err := platform.ReadJSONDoc(path)
	if err != nil {
		fmt.Fprintf(w, "%s está roto (%v); no se toca.\n", filepath.ToSlash(path), err)
		return 1
	}
	cur, ok := doc.Get("statusLine")
	if !exists || !ok {
		fmt.Fprintln(w, "No hay statusLine configurada.")
		return 1
	}
	m, _ := cur.(map[string]any)
	if !s.isOurs(m["command"]) {
		fmt.Fprintln(w, "La statusLine actual no es la de nxy; no se toca.")
		return 1
	}
	b, kept, err := platform.BackupAndPrune(path, s.now())
	if err != nil {
		fmt.Fprintf(w, "no pude hacer el backup de %s: %v; no se toca.\n", filepath.ToSlash(path), err)
		return 1
	}
	doc.Delete("statusLine")
	if _, err := platform.WriteJSONDoc(path, doc); err != nil {
		fmt.Fprintf(w, "no pude escribir %s: %v\n", filepath.ToSlash(path), err)
		return 1
	}
	fmt.Fprintf(w, "statusLine de nxy quitada de %s\n", filepath.ToSlash(path))
	fmt.Fprintf(w, "shim: %s (queda: lo usan el PATH y /nxy:tui)\n", filepath.ToSlash(stableShimPath(s.userDir)))
	fmt.Fprintf(w, "backup: %s (se conservan %d)\n", filepath.ToSlash(b), kept)
	fmt.Fprintln(w, "Reiniciá Claude Code para aplicarlo.")
	return 0
}

// runStatuslineSetup is `nxy statusline setup [install|remove]`.
func runStatuslineSetup(args []string, stdout, stderr io.Writer) int {
	s, err := realStatuslineSetup()
	if err != nil {
		fmt.Fprintf(stderr, "nxy statusline setup: %v\n", err)
		return 1
	}
	action := ""
	if len(args) > 0 {
		action = strings.ToLower(args[0])
	}
	switch action {
	case "", "show", "status":
		return s.show(stdout)
	case "install", "--apply":
		return s.install(stdout)
	case "remove":
		return s.remove(stdout)
	}
	fmt.Fprintln(stdout, "uso: nxy statusline setup [install|remove]")
	return 1
}

// ensureStableShim keeps the stable shim equal to this binary's (SessionStart) and creates it when
// missing: the statusline, the PATH entry and /nxy:tui all run it. Cost: one read of ~13.5 KB, and a
// write only when the version changed. No exec bit needed: on Linux/macOS it is always run with sh.
func ensureStableShim(userDir string) error {
	p := stableShimPath(userDir)
	cur, err := os.ReadFile(p)
	if err == nil && bytes.Equal(cur, nxydev.Shim) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return platform.WriteAtomic(p, nxydev.Shim)
}
