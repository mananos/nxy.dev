package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/mananos/nxy.dev/internal/app"
	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/platform"
	"github.com/mananos/nxy.dev/internal/worktree"
)

// The worktree actions of `nxy feature` (hosts/claude-code/entries/features.mjs). Text output; exit 1 on
// any error. Nothing here commits or pushes, and pause only leaves a mark: the handoff comes with the
// v2 memory.

const featureUsage = "uso: nxy feature list | new --name <n> --base <rama> [--issue <n>] | new --branch <b> | pause|resume --path <wt> | pause|resume --feature <id> | close --path <wt> [--force] | close [--feature <id>] [--force] | rule \"<text>\" --question \"<q>\" | stop [--feature <id>]"

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
	featureID := fs.String("feature", "", "feature id")
	force := fs.Bool("force", false, "close even with uncommitted changes")
	if err := fs.Parse(args); err != nil {
		return 1, true
	}
	given := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { given[f.Name] = true })
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "nxy feature %s: argumento inesperado %q\n%s\n", action, fs.Arg(0), featureUsage)
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
		on := action == "pause"
		hasFeature := given["feature"] && *featureID != ""
		hasPath := given["path"] && *path != ""
		if !hasFeature && !hasPath {
			return fail(fmt.Errorf("falta --path o --feature")), true
		}
		var feat domain.Feature
		foundFeature := false
		if hasFeature || hasPath {
			h, st := newHooksStore()
			if hasFeature {
				f, err := h.Store.Feature(ctx, *featureID)
				if err != nil {
					st.Close()
					return fail(fmt.Errorf("no hay una feature %s", *featureID)), true
				}
				feat, foundFeature = f, true
			} else {
				feat, foundFeature = featureByPath(ctx, h, *path)
			}
			if foundFeature {
				var err error
				if on {
					feat, err = h.PauseFeature(ctx, feat.ID)
				} else {
					feat, err = h.ResumeFeature(ctx, feat.ID)
				}
				st.Close()
				if err != nil {
					return fail(err), true
				}
				return featurePaused(ctx, feat, on, dir, shell, stdout, fail), true
			}
			st.Close()
		}
		if !hasPath {
			return fail(fmt.Errorf("no hay una feature %s", *featureID)), true
		}
		wt, _, _, ok := platform.FindWorktree(ctx, dir, *path)
		if !ok {
			return fail(fmt.Errorf("ese worktree no está en la lista")), true
		}
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
		if given["feature"] || !given["path"] || *path == "" {
			return featureClose(ctx, dir, *featureID, *force, shell, stdout, fail), true
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
	if f, found := featureOfCheckout(ctx, h, wtPath); found {
		feature = " --feature " + f.ID
	}
	return fmt.Sprintf("Para el modelo: preguntale al usuario, sin insistir, «¿Algo para la próxima sesión?». Si contesta, guardalo con `%s note save \"<título>\" --cwd \"%s\"%s` (el texto por stdin, 2 KB como máximo); si no, no hagas nada más.",
		exePath(), wtPath, feature)
}

// featureByPath is the active or paused feature that works at path (a checkout of one of its repos).
func featureByPath(ctx context.Context, h app.Hooks, path string) (domain.Feature, bool) {
	if f, found := featureOfCheckout(ctx, h, path); found {
		return f, true
	}
	for _, paused := range []bool{true, false} {
		fs, err := h.Store.Features(ctx, app.FeatureFilter{Active: true, Paused: paused})
		if err != nil {
			continue
		}
		for _, f := range fs {
			for _, r := range f.RepoList() {
				if platform.SamePath(r.Checkout, path) && (len(f.Repos) > 0 || f.Paused) {
					return f, true
				}
			}
		}
	}
	return domain.Feature{}, false
}

// featurePaused answers a pause or resume of a whole feature. With worktrees the mark of each one is
// left too; a feature in the session's folder has nothing to mark.
func featurePaused(ctx context.Context, f domain.Feature, on bool, dir string, shell worktree.Shell, stdout io.Writer, fail func(error) int) int {
	if f.UsesWorktrees() {
		for _, r := range f.Repos {
			if r.Checkout == "" || r.Undeclared {
				continue
			}
			if err := platform.SetPaused(r.Checkout, on, time.Now()); err != nil {
				return fail(err)
			}
		}
	}
	if !on {
		fmt.Fprintf(stdout, "Listo: la feature %s sigue activa y vuelve a tener sus checkouts.\n", f.ID)
		return 0
	}
	fmt.Fprintf(stdout, "Listo: la feature %s quedó pausada y soltó sus checkouts: otra feature puede tomarlos. Se retoma con `nxy feature resume --feature %s`.\n", f.ID, f.ID)
	for _, r := range f.Repos {
		if f.UsesWorktrees() && r.Checkout != "" && !r.Undeclared {
			fmt.Fprintf(stdout, "Para abrir %s: %s\n", r.Name, worktree.OpenCommand(r.Checkout, shell))
		}
	}
	root := dir
	if len(f.Repos) > 0 && f.Repos[0].Checkout != "" {
		root = f.Repos[0].Checkout
	}
	fmt.Fprintln(stdout, pauseNoteLine(ctx, root))
	return 0
}

// featureClose closes the worktrees of a finished or paused feature (the one given, or the newest of
// this folder). It refuses an active one; a feature that worked in the folder has no worktrees.
func featureClose(ctx context.Context, dir, id string, force bool, shell worktree.Shell, stdout io.Writer, fail func(error) int) int {
	h, st := newHooksStore()
	defer st.Close()
	var f domain.Feature
	if id != "" {
		g, err := h.Store.Feature(ctx, id)
		if err != nil {
			return fail(fmt.Errorf("no hay una feature %s", id))
		}
		f = g
	} else {
		fs, err := h.Store.Features(ctx, app.FeatureFilter{Home: h.Repos.Checkout(dir)})
		if err != nil {
			return fail(err)
		}
		found := false
		for _, g := range fs {
			if g.Paused || g.State.Phase == domain.PhaseDone {
				f, found = g, true
				break
			}
		}
		if !found {
			return fail(fmt.Errorf("falta --path (o --feature <id>): no hay una feature terminada o pausada en esta carpeta"))
		}
	}
	if !f.Paused && f.State.Phase != domain.PhaseDone {
		return fail(fmt.Errorf("la feature %s sigue activa: terminala, pausala o frenala con `nxy feature stop` antes de cerrar sus worktrees", f.ID))
	}
	if !f.UsesWorktrees() {
		fmt.Fprintf(stdout, "La feature %s trabajó en la carpeta, no tiene worktrees que cerrar.\n", f.ID)
		return 0
	}
	var failed []string
	for _, r := range f.Repos {
		if r.Checkout == "" || r.Undeclared {
			continue
		}
		root := r.Root
		if root == "" {
			root = dir
		}
		cmd, err := platform.RemoveWorktree(ctx, root, r.Checkout, force, shell)
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", r.Name, err))
			continue
		}
		fmt.Fprintf(stdout, "Listo: worktree de %s cerrado (la rama %s no se borra).\nComando que corrió: %s\n", r.Name, r.Branch, cmd)
		if n := deleteWorktreeNotes(ctx, r.Checkout); n > 0 {
			fmt.Fprintf(stdout, "Se borraron %d notas temporales de %s.\n", n, r.Checkout)
		}
	}
	if len(failed) > 0 {
		return fail(fmt.Errorf("%s", strings.Join(failed, "; ")))
	}
	return 0
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
	if f, found := featureOfCheckout(ctx, h, wt.Path); found {
		return fmt.Errorf("tiene una feature de nxy sin terminar (%s): terminala, o frenala con `nxy feature stop` desde ese worktree", f.ID)
	}
	return nil
}

// featureOfCheckout is the active feature that holds the worktree at path (or, for a feature of
// before the checkouts, the one started from that folder).
func featureOfCheckout(ctx context.Context, h app.Hooks, path string) (domain.Feature, bool) {
	if f, found, err := h.Store.ActiveFeature(ctx, h.Repos.Checkout(path)); err == nil && found {
		return f, true
	}
	if f, found, err := h.Store.ActiveFeature(ctx, h.Repos.Key(path)); err == nil && found && platform.SamePath(f.ProjectDir, path) {
		return f, true
	}
	return domain.Feature{}, false
}

// featureListLines adds the active and paused features with their repos, branch and checkout.
// Only the features of dir are listed: its home or a repo whose root or checkout is dir or lies under it.
func featureListLines(ctx context.Context, dir string, stdout io.Writer) int {
	h, st := newHooksStore()
	defer st.Close()
	home := h.Repos.Checkout(dir)
	under := func(p string) bool {
		if p == "" {
			return false
		}
		k := h.Repos.Checkout(p)
		if k == "" || home == "" {
			return false
		}
		return k == home || strings.HasPrefix(k, strings.TrimRight(home, "/\\")+"/") || strings.HasPrefix(k, strings.TrimRight(home, "/\\")+string(filepath.Separator))
	}
	var all []domain.Feature
	for _, paused := range []bool{false, true} {
		fs, err := h.Store.Features(ctx, app.FeatureFilter{Active: true, Paused: paused})
		if err != nil {
			continue
		}
		for _, f := range fs {
			mine := f.Home == home
			for _, r := range f.Repos {
				if under(r.Root) || under(r.Checkout) {
					mine = true
				}
			}
			if mine {
				all = append(all, f)
			}
		}
	}
	if len(all) == 0 {
		return 0
	}
	fmt.Fprintf(stdout, "Features de nxy (%d):\n", len(all))
	for _, f := range all {
		first, _, _ := strings.Cut(f.Request, "\n")
		state := "activa"
		if f.Paused {
			state = "pausada"
		}
		fmt.Fprintf(stdout, "- %s (%s) %s\n", f.ID, state, strings.TrimSpace(first))
		lines := app.RepoLines(f)
		for _, l := range lines {
			fmt.Fprintf(stdout, "    %s\n", l)
		}
		if len(lines) == 0 {
			for _, r := range f.Repos {
				fmt.Fprintf(stdout, "    %s (%s)\n", r.Name, r.Root)
			}
		}
		if !f.UsesWorktrees() && len(f.Repos) > 0 {
			fmt.Fprintln(stdout, "    en la carpeta de la sesión, sin worktrees")
		}
	}
	return len(all)
}

func featureList(ctx context.Context, dir string, stdout io.Writer, fail func(error) int) int {
	l, err := platform.ListWorktrees(ctx, dir)
	if err != nil {
		// Not a git repo (a parent folder of several): the features of the folder still show.
		if featureListLines(ctx, dir, stdout) == 0 {
			return fail(err)
		}
		return 0
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
	featureListLines(ctx, dir, stdout)
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
