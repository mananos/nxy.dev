package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func noEnv(string) string { return "" }

func TestKnownCodegraphLocations(t *testing.T) {
	win := KnownCodegraphLocations("windows", `C:\h`, func(k string) string {
		if k == "LOCALAPPDATA" {
			return `C:\L`
		}
		return ""
	})
	if len(win) != 5 || !strings.HasSuffix(filepath.ToSlash(win[0]), "L/codegraph/current/bin/codegraph.cmd") {
		t.Fatalf("windows: %v", win)
	}
	lin := KnownCodegraphLocations("linux", "/h", func(k string) string {
		if k == "CODEGRAPH_BIN_DIR" {
			return "/b"
		}
		return ""
	})
	if filepath.ToSlash(lin[0]) != "/b/codegraph" || filepath.ToSlash(lin[len(lin)-1]) != "/h/.codegraph/current/bin/codegraph" {
		t.Fatalf("linux: %v", lin)
	}
}

func TestResolveLauncherShim(t *testing.T) {
	dir := t.TempDir()
	shim := filepath.Join(dir, "codegraph.cmd")
	js := filepath.Join(dir, "lib", "cli.js")
	node := filepath.Join(dir, "node.exe")
	body := "@echo off\r\n\"%~dp0\\node.exe\" \"%~dp0\\lib\\cli.js\" %*\r\n"
	have := map[string]bool{js: true, node: true}
	read := func(p string) ([]byte, error) {
		if p == shim {
			return []byte(body), nil
		}
		return nil, os.ErrNotExist
	}
	got, ok := ResolveLauncher("windows", shim, read, func(p string) bool { return have[p] })
	if !ok || !reflect.DeepEqual(got, []string{node, js}) {
		t.Fatalf("got %v %v", got, ok)
	}
	if _, ok := ResolveLauncher("windows", shim, read, func(string) bool { return false }); ok {
		t.Fatal("missing js must fail")
	}
	if got, ok := ResolveLauncher("linux", "/x/codegraph.cmd", read, nil); !ok || got[0] != "/x/codegraph.cmd" {
		t.Fatalf("non-windows: %v", got)
	}
	if got, ok := ResolveLauncher("windows", "c.exe", read, nil); !ok || len(got) != 1 {
		t.Fatalf("exe: %v", got)
	}
}

func TestParseAffected(t *testing.T) {
	got, err := ParseAffected("warn\n{\n \"changedFiles\": [\"a.ts\"],\n \"affectedTests\": [\"src\\\\a.spec.ts\", \"b.spec.ts\"]\n}\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1] != "b.spec.ts" {
		t.Fatalf("got %v", got)
	}
	if _, err := ParseAffected("nope"); err == nil {
		t.Fatal("want error")
	}
}

func TestHasIndex(t *testing.T) {
	root := t.TempDir()
	if HasIndex(root, noEnv) {
		t.Fatal("no index yet")
	}
	if err := os.Mkdir(filepath.Join(root, ".cg-win"), 0o755); err != nil {
		t.Fatal(err)
	}
	if HasIndex(root, noEnv) {
		t.Fatal("other name ignored without env")
	}
	if !HasIndex(root, func(k string) string {
		if k == "CODEGRAPH_DIR" {
			return ".cg-win"
		}
		return ""
	}) {
		t.Fatal("CODEGRAPH_DIR must be honored")
	}
}

// fake records calls and answers by subcommand.
type fake struct {
	calls  []string
	stdins map[string]string
	answer map[string][]func() (int, string, error)
	root   string
	made   bool // init creates the index dir
}

func (f *fake) exec(ctx context.Context, dir string, argv []string, stdin string) (int, string, error) {
	sub := argv[1]
	f.calls = append(f.calls, sub)
	if f.stdins == nil {
		f.stdins = map[string]string{}
	}
	f.stdins[sub] = stdin
	q := f.answer[sub]
	if len(q) == 0 {
		if sub == "init" {
			os.Mkdir(filepath.Join(f.root, ".codegraph"), 0o755)
		}
		return 0, "", nil
	}
	fn := q[0]
	if len(q) > 1 {
		f.answer[sub] = q[1:]
	}
	exit, out, err := fn()
	if sub == "init" && exit == 0 && err == nil {
		os.Mkdir(filepath.Join(f.root, ".codegraph"), 0o755)
	}
	return exit, out, err
}

func newGraph(t *testing.T, f *fake, excluded *int) *Graph {
	f.root = t.TempDir()
	return &Graph{Argv: []string{"cg"}, Getenv: noEnv, Exec: f.exec,
		Exclude: func(context.Context, string) error { *excluded++; return nil }}
}

func res(exit int, out string, err error) []func() (int, string, error) {
	return []func() (int, string, error){func() (int, string, error) { return exit, out, err }}
}

const affJSON = `{"affectedTests":["a.spec.ts"]}`

func TestLifecycleInitThenSync(t *testing.T) {
	f := &fake{answer: map[string][]func() (int, string, error){"affected": res(0, affJSON, nil)}}
	ex := 0
	g := newGraph(t, f, &ex)
	r := g.Affected(context.Background(), f.root, []string{"a.ts", "b.ts"})
	if r.Status != StatusInitialized || !reflect.DeepEqual(r.Tests, []string{"a.spec.ts"}) {
		t.Fatalf("got %+v", r)
	}
	if !reflect.DeepEqual(f.calls, []string{"init", "sync", "affected"}) || ex != 1 {
		t.Fatalf("calls %v exclude %d", f.calls, ex)
	}
	if f.stdins["affected"] != "a.ts\nb.ts\n" {
		t.Fatalf("stdin %q", f.stdins["affected"])
	}
	// Index exists now: no init, status OK.
	f.calls = nil
	if r := g.Affected(context.Background(), f.root, []string{"a.ts"}); r.Status != StatusOK {
		t.Fatalf("got %+v", r)
	}
	if !reflect.DeepEqual(f.calls, []string{"sync", "affected"}) {
		t.Fatalf("calls %v", f.calls)
	}
}

func TestLifecycleInitTimeoutUninit(t *testing.T) {
	f := &fake{answer: map[string][]func() (int, string, error){"init": res(-1, "", context.DeadlineExceeded)}}
	ex := 0
	g := newGraph(t, f, &ex)
	r := g.Affected(context.Background(), f.root, []string{"a.ts"})
	if r.Status != StatusInitTimeout || !reflect.DeepEqual(f.calls, []string{"init", "uninit"}) || ex != 0 {
		t.Fatalf("got %+v calls %v", r, f.calls)
	}
}

func TestLifecycleInitFailed(t *testing.T) {
	f := &fake{answer: map[string][]func() (int, string, error){"init": res(1, "", nil)}}
	ex := 0
	g := newGraph(t, f, &ex)
	r := g.Affected(context.Background(), f.root, []string{"a.ts"})
	if r.Status != StatusInitFailed || !reflect.DeepEqual(f.calls, []string{"init", "uninit"}) {
		t.Fatalf("got %+v calls %v", r, f.calls)
	}
}

func TestLifecycleLockedUnlockRetry(t *testing.T) {
	os := func(exit int) func() (int, string, error) {
		return func() (int, string, error) { return exit, "", nil }
	}
	f := &fake{answer: map[string][]func() (int, string, error){
		"sync":     {os(1), os(0)},
		"affected": res(0, affJSON, nil),
	}}
	ex := 0
	g := newGraph(t, f, &ex)
	mk(t, f.root)
	r := g.Affected(context.Background(), f.root, []string{"a.ts"})
	if r.Status != StatusOK || !reflect.DeepEqual(f.calls, []string{"sync", "unlock", "sync", "affected"}) {
		t.Fatalf("got %+v calls %v", r, f.calls)
	}

	// Still stuck after unlock: Locked on timeout, SyncFailed on exit.
	to := func() (int, string, error) { return -1, "", context.DeadlineExceeded }
	f2 := &fake{answer: map[string][]func() (int, string, error){"sync": {os(1), to}}}
	g2 := newGraph(t, f2, &ex)
	mk(t, f2.root)
	if r := g2.Affected(context.Background(), f2.root, []string{"a.ts"}); r.Status != StatusLocked {
		t.Fatalf("got %+v", r)
	}
	f3 := &fake{answer: map[string][]func() (int, string, error){"sync": {os(1), os(2)}}}
	g3 := newGraph(t, f3, &ex)
	mk(t, f3.root)
	if r := g3.Affected(context.Background(), f3.root, []string{"a.ts"}); r.Status != StatusSyncFailed {
		t.Fatalf("got %+v", r)
	}
}

func TestLifecycleQueryFailedAndMissing(t *testing.T) {
	f := &fake{answer: map[string][]func() (int, string, error){"affected": res(0, "garbage", nil)}}
	ex := 0
	g := newGraph(t, f, &ex)
	mk(t, f.root)
	if r := g.Affected(context.Background(), f.root, []string{"a.ts"}); r.Status != StatusQueryFailed {
		t.Fatalf("got %+v", r)
	}
	f.answer["affected"] = []func() (int, string, error){func() (int, string, error) { return 0, "", errors.New("boom") }}
	if r := g.Affected(context.Background(), f.root, []string{"a.ts"}); r.Status != StatusQueryFailed {
		t.Fatalf("got %+v", r)
	}
	if r := (&Graph{}).Affected(context.Background(), f.root, nil); r.Status != StatusMissing {
		t.Fatalf("got %+v", r)
	}
}

func TestStepDeadlineCappedByCtx(t *testing.T) {
	f := &fake{}
	var dl time.Time
	g := &Graph{Argv: []string{"cg"}, Getenv: noEnv, Exec: func(ctx context.Context, _ string, argv []string, _ string) (int, string, error) {
		dl, _ = ctx.Deadline()
		return 0, "", nil
	}}
	f.root = t.TempDir()
	mk(t, f.root)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	g.Affected(ctx, f.root, nil)
	if dl.IsZero() || time.Until(dl) > 3*time.Second {
		t.Fatalf("deadline %v not capped by ctx", dl)
	}
}

func mk(t *testing.T, root string) {
	t.Helper()
	if err := os.Mkdir(filepath.Join(root, ".codegraph"), 0o755); err != nil {
		t.Fatal(err)
	}
}
