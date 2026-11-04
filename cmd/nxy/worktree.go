package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/mananos/nxy.dev/internal/platform"
	"github.com/mananos/nxy.dev/internal/worktree"
)

// The worktree actions of `nxy feature` (hosts/claude-code/entries/features.mjs). Text output; exit 1 on
// any error. Nothing here commits or pushes, and pause only leaves a mark: the handoff comes with the
// v2 memory.

const featureUsage = "usage: nxy feature list | new --name <n> --base <rama> [--issue <n>] | new --branch <b> | pause|resume --path <wt> | close --path <wt> [--force] | rule \"<text>\" --question \"<q>\" | stop"

const maxListedBases = 20

func currentShell() worktree.Shell { return worktree.ShellOf(runtime.GOOS, os.Getenv("SHELL")) }

// featureWorktrees runs the worktree actions; handled is false for any other action.
func featureWorktrees(action string, args []string, stdout, stderr io.Writer) (code int, handled bool) {
	switch action {
	case "list", "new", "pause", "resume", "close":
	default:
		return 0, false
	}
	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "nxy feature %s: %v\n", action, err)
		return 1, true
	}
	ctx := context.Background()
	fail := func(err error) int {
		fmt.Fprintf(stderr, "nxy feature %s: %v\n", action, err)
		return 1
	}
	fs := flag.NewFlagSet("feature "+action, flag.ContinueOnError)
	fs.SetOutput(stderr)
	name := fs.String("name", "", "feature name")
	base := fs.String("base", "", "local branch the new branch comes from")
	issue := fs.String("issue", "", "issue number")
	branch := fs.String("branch", "", "existing local branch")
	path := fs.String("path", "", "worktree path")
	force := fs.Bool("force", false, "close even with uncommitted changes")
	if err := fs.Parse(args); err != nil {
		return 1, true
	}
	given := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { given[f.Name] = true })
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "nxy feature %s: unexpected argument %q\n%s\n", action, fs.Arg(0), featureUsage)
		return 1, true
	}
	shell := currentShell()

	switch action {
	case "list":
		return featureList(ctx, dir, stdout, fail), true
	case "new":
		in := worktree.NamesInput{}
		switch {
		case given["name"] && given["branch"]:
			return fail(fmt.Errorf("usá --name con --base, o --branch, no los dos")), true
		case !given["name"] && !given["branch"]:
			return fail(fmt.Errorf("falta --name con --base, o --branch")), true
		case given["name"]:
			if !given["base"] {
				return fail(fmt.Errorf("falta --base: elegí la rama de la que sale la rama nueva")), true
			}
			in = worktree.NamesInput{Name: name, Base: *base, Issue: *issue}
		default:
			if given["base"] || given["issue"] {
				return fail(fmt.Errorf("--base y --issue sólo van con --name")), true
			}
			in = worktree.NamesInput{Branch: branch}
		}
		added, err := platform.AddWorktree(ctx, dir, in, shell)
		if err != nil {
			return fail(err), true
		}
		fmt.Fprintf(stdout, "Listo: worktree creado en %s (rama %s).\nComando que corrió: %s\nPara abrirlo: %s\n",
			added.Path, added.Branch, added.Command, worktree.OpenCommand(added.Path, shell))
		return 0, true
	case "pause", "resume":
		if !given["path"] || *path == "" {
			return fail(fmt.Errorf("falta --path")), true
		}
		wt, _, _, ok := platform.FindWorktree(ctx, dir, *path)
		if !ok {
			return fail(fmt.Errorf("ese worktree no está en la lista")), true
		}
		on := action == "pause"
		if err := platform.SetPaused(wt.Path, on, time.Now()); err != nil {
			return fail(err), true
		}
		if !on {
			fmt.Fprintf(stdout, "Listo: %s ya no figura como pausado (se borró su marca).\n", wt.Path)
			return 0, true
		}
		fmt.Fprintf(stdout, "Listo: %s quedó marcado como pausado.\nSólo se dejó la marca (~100 B en su .nxy/local); no se escribió ningún handoff ni se tocó git.\nPara abrirlo: %s\n",
			wt.Path, worktree.OpenCommand(wt.Path, shell))
		fmt.Fprintln(stdout, pauseNoteLine(ctx, wt.Path))
		return 0, true
	default: // close
		if !given["path"] || *path == "" {
			return fail(fmt.Errorf("falta --path")), true
		}
		if err := checkNoActiveFeature(ctx, dir, *path); err != nil {
			return fail(err), true
		}
		wtPath := *path
		if wt, _, _, ok := platform.FindWorktree(ctx, dir, *path); ok {
			wtPath = wt.Path
		}
		cmd, err := platform.RemoveWorktree(ctx, dir, *path, *force, shell)
		if err != nil {
			return fail(err), true
		}
		fmt.Fprintf(stdout, "Listo: worktree cerrado. La rama no se borra.\nComando que corrió: %s\n", cmd)
		if n := deleteWorktreeNotes(ctx, wtPath); n > 0 {
			fmt.Fprintf(stdout, "Se borraron %d notas temporales de esa carpeta.\n", n)
		}
		return 0, true
	}
}

// pauseNoteLine is the last line of `pause`, addressed to the model: it asks the user, optionally,
// for something to leave for the next session and says how to save it.
func pauseNoteLine(ctx context.Context, wtPath string) string {
	feature := ""
	h, st := newHooksStore()
	defer st.Close()
	if f, found, err := h.Store.ActiveFeature(ctx, h.Repos.Key(wtPath)); err == nil && found && platform.SamePath(f.ProjectDir, wtPath) {
		feature = " --feature " + f.ID
	}
	return fmt.Sprintf("Para el modelo: preguntale al usuario, sin insistir, «¿Algo para la próxima sesión?». Si contesta, guardalo con `%s note save \"<título>\" --cwd \"%s\"%s` (el texto por stdin, 2 KB como máximo); si no, no hagas nada más.",
		exePath(), wtPath, feature)
}

// deleteWorktreeNotes drops the temporary notes of a closed worktree's folder and says how many.
func deleteWorktreeNotes(ctx context.Context, wtPath string) int {
	h, st := newHooksStore()
	defer st.Close()
	n, err := h.Memory.DeleteNotesUnder(ctx, wtPath, runtime.GOOS == "windows")
	if err != nil {
		return 0
	}
	return n
}

// checkNoActiveFeature refuses to close a worktree that holds a v2 feature that is not done. The
// main worktree and this session's one are left to RemoveWorktree, which says why they stay.
func checkNoActiveFeature(ctx context.Context, dir, path string) error {
	wt, _, root, ok := platform.FindWorktree(ctx, dir, path)
	if !ok || wt.Main || platform.SamePath(wt.Path, root) {
		return nil
	}
	h, st := newHooksStore()
	defer st.Close()
	f, found, err := h.Store.ActiveFeature(ctx, h.Repos.Key(wt.Path))
	if err != nil {
		return err
	}
	if found && platform.SamePath(f.ProjectDir, wt.Path) {
		return fmt.Errorf("tiene una feature de nxy sin terminar (%s): terminala, o frenala con `nxy feature stop` desde ese worktree", f.ID)
	}
	return nil
}

func featureList(ctx context.Context, dir string, stdout io.Writer, fail func(error) int) int {
	l, err := platform.ListWorktrees(ctx, dir)
	if err != nil {
		return fail(err)
	}
	now := time.Now().UnixMilli()
	fmt.Fprintf(stdout, "Worktrees de %s (%d):\n", l.RepoName, len(l.Rows))
	for _, r := range l.Rows {
		v := worktree.RowView(worktree.RowInput{
			Path: r.Path, Branch: r.Branch, Main: r.Main, Current: r.Current, Dirty: r.Dirty, Ahead: r.Ahead,
			PausedAt: r.PausedAt, MissingDeps: r.MissingDeps,
		}, now)
		line := fmt.Sprintf("- %s [%s]", v.Name, v.Branch)
		if len(v.Tags) > 0 {
			line += " (" + strings.Join(v.Tags, ", ") + ")"
		}
		fmt.Fprintln(stdout, line)
		fmt.Fprintf(stdout, "    %s\n", r.Path)
		for _, ln := range v.Lines {
			if ln == "sin handoff" {
				continue // the handoff arrives with the v2 memory
			}
			fmt.Fprintf(stdout, "    %s\n", ln)
		}
	}
	if l.DefaultBranch != "" {
		fmt.Fprintf(stdout, "Rama base por defecto: %s\n", l.DefaultBranch)
	}
	if len(l.Bases) > 0 {
		shown := l.Bases
		more := ""
		if len(shown) > maxListedBases {
			shown, more = shown[:maxListedBases], fmt.Sprintf(" … y %d más", len(l.Bases)-maxListedBases)
		}
		fmt.Fprintf(stdout, "Bases para `new --base`: %s%s\n", strings.Join(shown, ", "), more)
	}
	if len(l.Branches.Free) > 0 {
		shown := l.Branches.Free
		more := ""
		if len(shown) > maxListedBases {
			shown, more = shown[:maxListedBases], fmt.Sprintf(" … y %d más", len(l.Branches.Free)-maxListedBases)
		}
		fmt.Fprintf(stdout, "Ramas libres para `new --branch`: %s%s\n", strings.Join(shown, ", "), more)
	}
	return 0
}
