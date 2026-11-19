package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	nxydev "github.com/mananos/nxy.dev/v2"
	"github.com/mananos/nxy.dev/v2/internal/platform"
	"github.com/mananos/nxy.dev/v2/internal/store/sqlite"
	"github.com/mananos/nxy.dev/v2/internal/tui"
	"github.com/mananos/nxy.dev/v2/internal/views"
)

// runTui opens the TUI over the real data (views.Source of the db), or over the sample views with
// --demo.
func runTui(args []string, _ io.Reader, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "where" {
		return tuiWhere(stdout, stderr)
	}
	fs := flag.NewFlagSet("tui", flag.ContinueOnError)
	fs.SetOutput(stderr)
	demo := fs.Bool("demo", false, "open the TUI with sample data")
	dump := fs.String("dump", "", "print a screen and exit")
	feature := fs.String("feature", "", "what the screen shows: a feature id, a memory scope or a file (default: the one of the screen); with --path, the feature of the file")
	filePath := fs.String("path", "", "dump archivo: the file, as the plan writes it (repo/path)")
	cwd := fs.String("cwd", "", "the folder; default CLAUDE_PROJECT_DIR or the current one")
	width := fs.Int("width", 120, "width of the dump in cells")
	plain := fs.Bool("plain", false, "no ANSI in the dump")
	icons := fs.String("icons", "auto", "auto|nerd|unicode")
	if err := fs.Parse(args); err != nil {
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
		fmt.Fprintf(stderr, "nxy tui: --icons %q: usá auto, nerd o unicode\n", *icons)
		return 1
	}
	o := tui.Options{Width: *width, Nerd: nerd, Plain: *plain}
	if *demo {
		src, err := tui.NewJSONSource(nxydev.DemoViews, "testdata/views")
		if err != nil {
			fmt.Fprintf(stderr, "nxy tui: %v\n", err)
			return 1
		}
		o.Source, o.InRepo, o.Demo = src, true, true
	} else {
		dir := resolveCwd(setupArgs{cwd: *cwd})
		src, closeSrc, code := openRealSource(dir, stderr)
		if code != 0 {
			return code
		}
		defer closeSrc()
		o.Source, o.InRepo, o.Run = src, src.InRepo(), runSelf(dir, "")
		if *dump == "" {
			w, err := src.store.Watch(context.Background())
			if err != nil {
				fmt.Fprintf(stderr, "nxy tui: %v\n", err)
				return 1
			}
			defer w.Close()
			o.Watch = w.Poll
			// the base version is read before the first load, so a write in between is seen
			if v, _, perr := w.Poll(context.Background()); perr == nil {
				o.Ver, o.HaveVer = v, true
			}
			schema, serr := sqlite.LatestSchemaVersion()
			if serr != nil {
				fmt.Fprintf(stderr, "nxy tui: %v\n", serr)
				return 1
			}
			o.Schema = schema
			// the hook only waits for answers while this mark is fresh
			if run, rerr := platform.RunDir(); rerr == nil {
				platform.SweepHeartbeats(run, time.Now())
				nonce, stopBeat := platform.HeartbeatNonce(run)
				defer stopBeat()
				o.Run = runSelf(dir, nonce)
				// a newer nxy migrated the db: this TUI answers nothing, so it stops counting as alive
				o.OnStale = stopBeat
			}
		}
	}
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
	key := *feature
	if strings.HasPrefix(*dump, "archivo") && *filePath != "" {
		key, o.FileFeature = *filePath, *feature
	}
	out, err := tui.Render(o, *dump, key)
	if err != nil {
		fmt.Fprintf(stderr, "nxy tui: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, strings.TrimRight(out, "\n"))
	return 0
}

// tuiWhere is `nxy tui where`: it makes sure the shim exists and prints the command to paste in
// another terminal. It opens no terminal.
func tuiWhere(stdout, stderr io.Writer) int {
	u, err := platform.UserDir()
	if err == nil {
		err = ensureStableShim(u)
	}
	if err != nil {
		fmt.Fprintf(stderr, "nxy tui where: %v\n", err)
		return 1
	}
	if nxyOnPath() != "" {
		fmt.Fprintln(stdout, "Pegá en una terminal:\n\n  nxy tui")
		return 0
	}
	shim := filepath.ToSlash(stableShimPath(u))
	fmt.Fprintln(stdout, "Pegá en una terminal:")
	fmt.Fprintf(stdout, "\n  PowerShell:                         & \"%s\" tui\n", shim)
	fmt.Fprintf(stdout, "  Git Bash, Linux y macOS:            sh \"%s\" tui\n", shim)
	fmt.Fprintln(stdout, "\nPara que sea `nxy tui`: /nxy:setup → nxy en el PATH (o `nxy setup run path --yes`).")
	return 0
}

// selfArgv is the argv of a button without the leading "nxy": the TUI runs it as this same
// binary.
func selfArgv(argv []string) ([]string, error) {
	if len(argv) == 0 || argv[0] != "nxy" {
		return nil, fmt.Errorf("comando que no es de nxy: %q", argv)
	}
	return argv[1:], nil
}

// actionTimeout bounds every button's command (winget can take a while).
const actionTimeout = 10 * time.Minute

// selfEnv is the environment of a button's subprocess: env plus the TUI's nonce (the proof that
// `nxy action decide` is the user's), replacing any nonce already there.
func selfEnv(env []string, nonce string) []string {
	out := make([]string, 0, len(env)+1)
	for _, e := range env {
		if !strings.HasPrefix(e, platform.NonceEnv+"=") {
			out = append(out, e)
		}
	}
	if nonce != "" {
		out = append(out, platform.NonceEnv+"="+nonce)
	}
	return out
}

// runSelf runs a button's command as a subprocess of this binary, in dir. stdin is empty so it
// never takes keys from the TUI; stdout and stderr come back together.
func runSelf(dir, nonce string) func(ctx context.Context, argv []string) (string, error) {
	return func(ctx context.Context, argv []string) (string, error) {
		rest, err := selfArgv(argv)
		if err != nil {
			return "", err
		}
		exe, err := os.Executable()
		if err != nil {
			return "", err
		}
		ctx, cancel := context.WithTimeout(ctx, actionTimeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, exe, rest...)
		cmd.Dir = dir
		cmd.Env = selfEnv(os.Environ(), nonce)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
}

// openRealSource opens the db and the source over it; a db from a newer nxy is not touched.
func openRealSource(dir string, stderr io.Writer) (*viewSource, func(), int) {
	path, err := platform.DBPath()
	if err != nil {
		fmt.Fprintf(stderr, "nxy tui: %v\n", err)
		return nil, nil, 1
	}
	st, err := sqlite.Open(context.Background(), path, nil)
	if errors.Is(err, sqlite.ErrSchemaNewer) {
		fmt.Fprintln(stderr, "nxy tui: la db es de un nxy más nuevo: abrí la TUI de esa versión")
		return nil, nil, 1
	}
	if err != nil {
		fmt.Fprintf(stderr, "nxy tui: %v\n", err)
		return nil, nil, 1
	}
	src := newViewSource(st, os.Getenv, dir, time.Now, time.Local)
	return src, func() { src.Close(); st.Close() }, 0
}

var _ views.Source = (*viewSource)(nil)
