package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/mananos/nxy.dev/v2/internal/app"
	"github.com/mananos/nxy.dev/v2/internal/memory"
	"github.com/mananos/nxy.dev/v2/internal/platform"
	"github.com/mananos/nxy.dev/v2/internal/store/sqlite"
)

// app cannot import adapters: the store meets the port here.
var _ app.MemoryStore = (*sqlite.Store)(nil)

const memUsage = `uso: nxy mem <rule "<text>" --question "<q>" [--paths g1,g2] [--origin chat] [--feature id] [--supersedes K3] [--global [--roles planner,implementer,reviewer,main]] | edit <K3|G2|A7> [--text "<t>"] [--question "<q>"] [--paths g1,g2] [--roles a,b] [--files a,b] | rm <ref> | restore <ref> | keep <ref> | show | search <text> [--all] | promote <A7> --question "<q>" [--paths g1,g2] | related <files...> | export [--out file] [--notes] | import <file> [--apply] | note "<text>" [--files a,b] | bootstrap [save] (a repo's first rules; save reads the scout JSON on stdin)>`

const noteUsage = `uso: nxy note <save "<title>" [--id N2] [--feature id] [--session id: carpeta y cadena de esa sesión] (el cuerpo por stdin) | list [--all] | done <id>>`

// openMemory opens the global database and wires the memory use cases.
func openMemory(ctx context.Context) (app.Memory, *sqlite.Store, error) {
	path, err := platform.DBPath()
	if err != nil {
		return app.Memory{}, nil, err
	}
	s, err := sqlite.Open(ctx, path, nil)
	if err != nil {
		return app.Memory{}, nil, err
	}
	return app.Memory{Store: s, Repos: repoKeys{}, FoldCase: runtime.GOOS == "windows"}, s, nil
}

// memArgs reads the flags of mem and note. The bool flags are taken out first so that a bare
// `--apply file` does not read the file as the value of the flag.
func memArgs(args []string, bools ...string) (opts map[string]string, positional []string, flags map[string]bool) {
	flags = map[string]bool{}
	var rest []string
next:
	for _, a := range args {
		for _, b := range bools {
			if a == "--"+b {
				flags[b] = true
				continue next
			}
		}
		rest = append(rest, a)
	}
	opts, positional = parseLocateArgs(rest)
	return opts, positional, flags
}

func memCwd(opts map[string]string) string {
	cwd := opts["cwd"]
	if cwd == "true" {
		cwd = ""
	}
	return resolveCwd(setupArgs{cwd: cwd})
}

func optVal(opts map[string]string, k string) string {
	if v := opts[k]; v != "true" {
		return v
	}
	return ""
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// repoRelative turns files into paths relative to the git root of cwd, with "/".
func repoRelative(cwd string, files []string) []string {
	root, ok := platform.GitRoot(cwd)
	if !ok {
		root = cwd
	}
	var out []string
	for _, f := range files {
		p := filepath.FromSlash(f)
		if !filepath.IsAbs(p) {
			p = filepath.Join(cwd, p)
		}
		if rel, err := filepath.Rel(root, p); err == nil {
			p = rel
		}
		out = append(out, memory.CleanPath(filepath.ToSlash(p)))
	}
	return out
}

func memFail(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "nxy: %v\n", err)
	return 1
}

// runMem is `nxy mem`: the memory of the repo (rules, learnings, related work, export/import).
func runMem(args []string, stdin io.Reader, stdout, stderr io.Writer) (code int) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(stderr, "nxy mem: %v\n", r)
			code = 1
		}
	}()
	if len(args) == 0 {
		fmt.Fprintln(stderr, memUsage)
		return 1
	}
	sub := args[0]
	opts, pos, flags := memArgs(args[1:], "all", "notes", "apply", "global")
	cwd := memCwd(opts)
	ctx := context.Background()
	switch sub {
	case "rule", "show", "search", "promote", "related", "export", "import", "note", "bootstrap", "edit", "rm", "restore", "keep":
	default:
		fmt.Fprintln(stderr, memUsage)
		return 1
	}
	m, st, err := openMemory(ctx)
	if err != nil {
		return memFail(stderr, err)
	}
	defer st.Close()

	switch sub {
	case "bootstrap":
		return runMemBootstrap(ctx, st, cwd, pos, stdin, stdout, stderr)
	case "rule":
		if len(pos) == 0 {
			fmt.Fprintln(stderr, memUsage)
			return 1
		}
		if flags["global"] {
			r, err := m.AddGlobalRule(ctx, strings.Join(pos, " "), optVal(opts, "question"), optVal(opts, "origin"),
				splitList(optVal(opts, "paths")), splitList(optVal(opts, "roles")), optVal(opts, "supersedes"))
			if err != nil {
				return memFail(stderr, err)
			}
			fmt.Fprintf(stdout, "guardé la regla global %s\n", r.Short)
			return 0
		}
		if _, ok := opts["roles"]; ok {
			return memFail(stderr, errors.New("--roles sólo va con reglas globales (G<n>): agregá --global"))
		}
		r, err := m.AddRule(ctx, cwd, strings.Join(pos, " "), optVal(opts, "question"), optVal(opts, "origin"),
			splitList(optVal(opts, "paths")), optVal(opts, "feature"), optVal(opts, "supersedes"))
		if err != nil {
			return memFail(stderr, err)
		}
		fmt.Fprintf(stdout, "guardé la regla %s\n", r.Short)
	case "edit":
		if len(pos) != 1 {
			fmt.Fprintln(stderr, memUsage)
			return 1
		}
		short, err := runMemEdit(ctx, m, cwd, pos[0], opts)
		if err != nil {
			return memFail(stderr, err)
		}
		fmt.Fprintf(stdout, "edité %s\n", short)
	case "keep":
		if len(pos) != 1 {
			fmt.Fprintln(stderr, memUsage)
			return 1
		}
		short, err := m.Keep(ctx, pos[0])
		if err != nil {
			return memFail(stderr, err)
		}
		fmt.Fprintf(stdout, "conservé %s: nxy no la vuelve a proponer\n", short)
	case "rm", "restore":
		if len(pos) != 1 {
			fmt.Fprintln(stderr, memUsage)
			return 1
		}
		if sub == "rm" {
			short, err := m.Remove(ctx, pos[0])
			if err != nil {
				return memFail(stderr, err)
			}
			fmt.Fprintf(stdout, "borré %s (nxy mem restore %s lo recupera)\n", short, short)
		} else {
			short, err := m.Restore(ctx, pos[0])
			if err != nil {
				return memFail(stderr, err)
			}
			fmt.Fprintf(stdout, "recuperé %s\n", short)
		}
	case "show":
		rules, learnings, err := m.Show(ctx, cwd)
		if err != nil {
			return memFail(stderr, err)
		}
		fmt.Fprint(stdout, memory.RenderShow(rules, learnings, time.Now()))
		grules, glearnings, err := m.ShowGlobal(ctx)
		if err != nil {
			return memFail(stderr, err)
		}
		fmt.Fprint(stdout, memory.RenderGlobal(grules, glearnings, time.Now()))
	case "search":
		hits, err := m.Search(ctx, cwd, strings.Join(pos, " "), flags["all"])
		if err != nil {
			return memFail(stderr, err)
		}
		fmt.Fprint(stdout, memory.RenderSearch(hits))
	case "promote":
		if len(pos) == 0 {
			fmt.Fprintln(stderr, memUsage)
			return 1
		}
		r, err := m.Promote(ctx, pos[0], optVal(opts, "question"), splitList(optVal(opts, "paths")))
		if err != nil {
			return memFail(stderr, err)
		}
		fmt.Fprintf(stdout, "promoví %s a la regla %s\n", pos[0], r.Short)
	case "related":
		fs, ls, err := m.Related(ctx, cwd, repoRelative(cwd, pos))
		if err != nil {
			return memFail(stderr, err)
		}
		fmt.Fprint(stdout, memory.RenderRelated(fs, ls))
	case "export":
		b, err := m.Export(ctx, flags["notes"])
		if err != nil {
			return memFail(stderr, err)
		}
		data, err := memory.EncodeBundle(b)
		if err != nil {
			return memFail(stderr, err)
		}
		if out := optVal(opts, "out"); out != "" {
			if err := platform.WriteAtomic(platform.NativePath(out), data); err != nil {
				return memFail(stderr, err)
			}
			fmt.Fprintf(stdout, "exporté a %s\n", out)
		} else {
			stdout.Write(data)
			fmt.Fprintln(stdout)
		}
	case "import":
		if len(pos) == 0 {
			fmt.Fprintln(stderr, memUsage)
			return 1
		}
		var r io.Reader = stdin
		if pos[0] != "-" {
			f, err := os.Open(platform.NativePath(pos[0]))
			if err != nil {
				return memFail(stderr, err)
			}
			defer f.Close()
			r = f
		}
		d, err := m.ImportPreview(ctx, r)
		if err != nil {
			return memFail(stderr, err)
		}
		if flags["apply"] {
			if err := m.ImportApply(ctx, d); err != nil {
				return memFail(stderr, err)
			}
		}
		fmt.Fprint(stdout, memory.RenderDiff(d, flags["apply"]))
	case "note":
		if len(pos) == 0 {
			fmt.Fprintln(stderr, memUsage)
			return 1
		}
		l, err := m.AddLearning(ctx, cwd, strings.Join(pos, " "), repoRelative(cwd, splitList(optVal(opts, "files"))))
		var already *memory.AlreadyRuleError
		if errors.As(err, &already) {
			fmt.Fprintln(stdout, already.Error())
			return 0
		}
		if err != nil {
			return memFail(stderr, err)
		}
		fmt.Fprintf(stdout, "guardé el aprendizaje %s (visto %d)\n", l.Short, l.Seen)
	}
	return 0
}

// runMemEdit builds the edit from the flags present and applies it to a rule (K3, G2) or a learning (A7).
func runMemEdit(ctx context.Context, m app.Memory, cwd, ref string, opts map[string]string) (string, error) {
	// a flag given with no value is an error; an empty value of --paths, --roles or --files clears them
	val := func(k string) (string, bool, error) {
		v, ok := opts[k]
		if ok && v == "true" {
			return "", true, fmt.Errorf("--%s necesita un valor", k)
		}
		return v, ok, nil
	}
	list := func(k string) (*[]string, error) {
		v, ok, err := val(k)
		if !ok || err != nil {
			return nil, err
		}
		l := splitList(v)
		if l == nil {
			l = []string{}
		}
		return &l, nil
	}
	str := func(k string) (*string, error) {
		v, ok, err := val(k)
		if !ok || err != nil {
			return nil, err
		}
		return &v, nil
	}
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", errors.New("edit necesita una referencia (K3, G2 o A7)")
	}
	learning, err := m.IsLearning(ctx, ref)
	if err != nil {
		return "", err
	}
	if learning {
		for _, k := range []string{"question", "paths", "roles"} {
			if _, ok := opts[k]; ok {
				return "", fmt.Errorf("--%s no va con un aprendizaje (A<n>): usá --text o --files", k)
			}
		}
		text, err := str("text")
		if err != nil {
			return "", err
		}
		files, err := list("files")
		if err != nil {
			return "", err
		}
		if files != nil {
			rel := repoRelative(cwd, *files)
			files = &rel
		}
		e := memory.LearningEdit{Text: text, Files: files}
		if e.Empty() {
			return "", errors.New("no hay nada que cambiar: pasá --text o --files")
		}
		l, err := m.EditLearning(ctx, ref, e)
		return l.Short, err
	}
	if _, ok := opts["files"]; ok {
		return "", errors.New("--files sólo va con un aprendizaje (A<n>): una regla usa --paths")
	}
	text, err := str("text")
	if err != nil {
		return "", err
	}
	question, err := str("question")
	if err != nil {
		return "", err
	}
	paths, err := list("paths")
	if err != nil {
		return "", err
	}
	roles, err := list("roles")
	if err != nil {
		return "", err
	}
	e := memory.RuleEdit{Text: text, Question: question, Paths: paths, Roles: roles}
	if e.Empty() {
		return "", errors.New("no hay nada que cambiar: pasá --text, --question, --paths o --roles")
	}
	r, err := m.EditRule(ctx, ref, e)
	return r.Short, err
}

// runMemBootstrap is `nxy mem bootstrap [save]`: the bare form starts the scouting and prints what
// the main thread does next; `save` takes the scout's JSON candidates from stdin.
func runMemBootstrap(ctx context.Context, st *sqlite.Store, cwd string, pos []string, stdin io.Reader, stdout, stderr io.Writer) int {
	repo := repoKeys{}.Key(cwd)
	cur, _, err := st.Bootstrap(ctx, repo)
	if err != nil {
		return memFail(stderr, err)
	}
	cur.Repo = repo
	switch {
	case len(pos) == 0:
		if cur.OfferedAt == 0 {
			cur.OfferedAt = time.Now().UnixMilli()
		}
		cur.Status, cur.Candidates = "scouting", nil
		if err := st.SaveBootstrap(ctx, cur); err != nil {
			return memFail(stderr, err)
		}
		fmt.Fprintln(stdout, app.BootstrapDispatch())
	case len(pos) == 1 && pos[0] == "save":
		if cur.Status != "scouting" {
			return memFail(stderr, errors.New("bootstrap is not scouting; run `nxy mem bootstrap` first"))
		}
		data, err := io.ReadAll(stdin)
		if err != nil {
			return memFail(stderr, err)
		}
		cands, err := memory.DecodeBootstrap(data)
		if err != nil {
			return memFail(stderr, err)
		}
		cur.Status, cur.Candidates = "confirming", cands
		if err := st.SaveBootstrap(ctx, cur); err != nil {
			return memFail(stderr, err)
		}
		fmt.Fprintf(stdout, "saved %d candidates; nxy asks the user which to keep\n", len(cands))
	default:
		fmt.Fprintln(stderr, memUsage)
		return 1
	}
	return 0
}

// runNote is `nxy note`: temporary notes, kept by folder (not by git repo) for 30 days.
func runNote(args []string, stdin io.Reader, stdout, stderr io.Writer) (code int) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(stderr, "nxy note: %v\n", r)
			code = 1
		}
	}()
	if len(args) == 0 {
		fmt.Fprintln(stderr, noteUsage)
		return 1
	}
	sub := args[0]
	opts, pos, flags := memArgs(args[1:], "all")
	cwd := memCwd(opts)
	ctx := context.Background()
	switch sub {
	case "save", "list", "done":
	default:
		fmt.Fprintln(stderr, noteUsage)
		return 1
	}
	m, st, err := openMemory(ctx)
	if err != nil {
		return memFail(stderr, err)
	}
	defer st.Close()

	switch sub {
	case "save":
		if len(pos) == 0 {
			fmt.Fprintln(stderr, noteUsage)
			return 1
		}
		body, err := io.ReadAll(io.LimitReader(stdin, 1<<20))
		if err != nil {
			return memFail(stderr, err)
		}
		session := optVal(opts, "session")
		folder := cwd
		if session != "" {
			// the note belongs to the folder the session works in, wherever the command runs
			if row, ok, err := st.Session(ctx, session); err == nil && ok && row.Folder != "" {
				folder = row.Folder
			}
		}
		branch, base, fork := platform.NoteBranchOf(ctx, folder)
		n, err := m.NoteSave(ctx, folder, strings.Join(pos, " "), string(body), optVal(opts, "id"), optVal(opts, "feature"), session, branch, base, fork)
		if err == nil && session != "" {
			_ = st.ChainNote(ctx, session, n.Short)
		}
		if err != nil {
			return memFail(stderr, err)
		}
		fmt.Fprintf(stdout, "guardé la nota %s\n", n.Short)
	case "list":
		notes, err := m.NoteList(ctx, cwd, flags["all"])
		if err != nil {
			return memFail(stderr, err)
		}
		fmt.Fprint(stdout, memory.RenderNotes(notes, time.Now()))
	case "done":
		if len(pos) == 0 {
			fmt.Fprintln(stderr, noteUsage)
			return 1
		}
		if err := m.NoteDone(ctx, pos[0]); err != nil {
			return memFail(stderr, err)
		}
		fmt.Fprintf(stdout, "nota %s hecha\n", pos[0])
	}
	return 0
}
