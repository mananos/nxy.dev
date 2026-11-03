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

	"github.com/mananos/nxy.dev/internal/app"
	"github.com/mananos/nxy.dev/internal/memory"
	"github.com/mananos/nxy.dev/internal/platform"
	"github.com/mananos/nxy.dev/internal/store/sqlite"
)

// app cannot import adapters: the store meets the port here.
var _ app.MemoryStore = (*sqlite.Store)(nil)

const memUsage = `usage: nxy mem <rule "<text>" --question "<q>" [--paths g1,g2] [--origin chat] [--feature id] [--supersedes K3] | show | search <text> [--all] | promote <A7> --question "<q>" [--paths g1,g2] | related <files...> | export [--out file] [--notes] | import <file> [--apply] | note "<text>" [--files a,b]>`

const noteUsage = `usage: nxy note <save "<title>" [--id N2] [--feature id] [--session id] (body on stdin) | list [--all] | done <id>>`

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
	opts, pos, flags := memArgs(args[1:], "all", "notes", "apply")
	cwd := memCwd(opts)
	ctx := context.Background()
	switch sub {
	case "rule", "show", "search", "promote", "related", "export", "import", "note":
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
	case "rule":
		if len(pos) == 0 {
			fmt.Fprintln(stderr, memUsage)
			return 1
		}
		r, err := m.AddRule(ctx, cwd, strings.Join(pos, " "), optVal(opts, "question"), optVal(opts, "origin"),
			splitList(optVal(opts, "paths")), optVal(opts, "feature"), optVal(opts, "supersedes"))
		if err != nil {
			return memFail(stderr, err)
		}
		fmt.Fprintf(stdout, "saved rule %s\n", r.Short)
	case "show":
		rules, learnings, err := m.Show(ctx, cwd)
		if err != nil {
			return memFail(stderr, err)
		}
		fmt.Fprint(stdout, memory.RenderShow(rules, learnings, time.Now()))
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
		fmt.Fprintf(stdout, "promoted %s to rule %s\n", pos[0], r.Short)
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
			fmt.Fprintf(stdout, "exported to %s\n", out)
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
		fmt.Fprintf(stdout, "saved learning %s (seen %d)\n", l.Short, l.Seen)
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
		n, err := m.NoteSave(ctx, cwd, strings.Join(pos, " "), string(body), optVal(opts, "id"), optVal(opts, "feature"), optVal(opts, "session"))
		if err != nil {
			return memFail(stderr, err)
		}
		fmt.Fprintf(stdout, "saved note %s\n", n.Short)
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
		fmt.Fprintf(stdout, "note %s done\n", pos[0])
	}
	return 0
}
