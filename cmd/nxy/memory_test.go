package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runCLI runs `nxy <args>` with stdin and returns exit code, stdout and stderr.
func runCLI(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := run(args, strings.NewReader(stdin), &out, &errb)
	return code, out.String(), errb.String()
}

func mustCLI(t *testing.T, stdin string, args ...string) string {
	t.Helper()
	code, out, errs := runCLI(t, stdin, args...)
	if code != 0 {
		t.Fatalf("nxy %v: exit %d: %s%s", args, code, out, errs)
	}
	return out
}

func TestMemRuleNeedsQuestion(t *testing.T) {
	isolate(t)
	work := t.TempDir()
	code, _, errs := runCLI(t, "", "mem", "rule", "never edit generated files", "--cwd", work)
	if code != 1 || !strings.Contains(errs, "question") {
		t.Fatalf("exit %d, stderr %q", code, errs)
	}
	if code, _, _ := runCLI(t, "", "mem", "bogus"); code != 1 {
		t.Fatalf("unknown subcommand: exit %d", code)
	}
}

func TestMemRuleShowSearchPromote(t *testing.T) {
	isolate(t)
	work := t.TempDir()
	mustCLI(t, "", "mem", "rule", "never edit generated files", "--question", "who owns generated code?", "--cwd", work)
	show := mustCLI(t, "", "mem", "show", "--cwd", work)
	if !strings.Contains(show, "never edit generated files") || !strings.Contains(show, "who owns generated code?") {
		t.Fatalf("show: %q", show)
	}
	if got := mustCLI(t, "", "mem", "search", "generated", "--cwd", work); !strings.Contains(got, "never edit generated files") {
		t.Fatalf("search: %q", got)
	}

	mustCLI(t, "", "mem", "note", "tests need the sqlite driver", "--cwd", work)
	out := mustCLI(t, "", "mem", "note", "tests need the sqlite driver", "--cwd", work)
	if !strings.Contains(out, "seen 2") {
		t.Fatalf("second note: %q", out)
	}
	if show := mustCLI(t, "", "mem", "show", "--cwd", work); !strings.Contains(show, "seen 2 times") {
		t.Fatalf("show: %q", show)
	}
	if code, _, _ := runCLI(t, "", "mem", "promote", "A1", "--cwd", work); code != 1 {
		t.Fatalf("promote without question: exit %d", code)
	}
	mustCLI(t, "", "mem", "promote", "A1", "--question", "what does the test need?", "--cwd", work)
	show = mustCLI(t, "", "mem", "show", "--cwd", work)
	if strings.Contains(show, "Learnings") || !strings.Contains(show, "what does the test need?") {
		t.Fatalf("after promote: %q", show)
	}
}

func TestMemNoteOnPromotedExitsZero(t *testing.T) {
	isolate(t)
	work := t.TempDir()
	mustCLI(t, "", "mem", "note", "tests need the sqlite driver", "--cwd", work)
	mustCLI(t, "", "mem", "promote", "A1", "--question", "what does the test need?", "--cwd", work)
	out := mustCLI(t, "", "mem", "note", "tests need the sqlite driver", "--cwd", work)
	if !strings.Contains(out, "already rule K1") {
		t.Fatalf("note on promoted: %q", out)
	}
}

func TestMemExportImport(t *testing.T) {
	isolate(t)
	work := t.TempDir()
	mustCLI(t, "", "mem", "rule", "keep handlers thin", "--question", "where does logic live?", "--cwd", work)
	mustCLI(t, "the body", "note", "save", "scratch idea", "--cwd", work)

	plain := mustCLI(t, "", "mem", "export", "--cwd", work)
	if strings.Contains(plain, "scratch idea") || !strings.Contains(plain, "keep handlers thin") {
		t.Fatalf("export without --notes: %q", plain)
	}
	if withNotes := mustCLI(t, "", "mem", "export", "--notes", "--cwd", work); !strings.Contains(withNotes, "scratch idea") {
		t.Fatalf("export with --notes: %q", withNotes)
	}

	file := filepath.Join(t.TempDir(), "mem.json")
	mustCLI(t, "", "mem", "export", "--out", file, "--cwd", work)
	if _, err := os.Stat(file); err != nil {
		t.Fatal(err)
	}

	other := t.TempDir()
	t.Setenv("NXY_HOME", other)
	t.Setenv("HOME", other)
	t.Setenv("USERPROFILE", other)
	dry := mustCLI(t, "", "mem", "import", file)
	if !strings.Contains(dry, "dry run") {
		t.Fatalf("dry run: %q", dry)
	}
	if show := mustCLI(t, "", "mem", "show", "--cwd", work); strings.Contains(show, "keep handlers thin") {
		t.Fatalf("dry run wrote: %q", show)
	}
	applied := mustCLI(t, "", "mem", "import", "--apply", file)
	if !strings.Contains(applied, "imported") {
		t.Fatalf("apply: %q", applied)
	}
	if show := mustCLI(t, "", "mem", "show", "--cwd", work); !strings.Contains(show, "keep handlers thin") {
		t.Fatalf("after apply: %q", show)
	}
	if again := mustCLI(t, "", "mem", "import", file, "--apply"); !strings.Contains(again, "nothing to import") {
		t.Fatalf("second apply: %q", again)
	}

	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte(`{"version":2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _, errs := runCLI(t, "", "mem", "import", bad); code != 1 || !strings.Contains(errs, "unsupported export version") {
		t.Fatalf("bad version: exit %d, %q", code, errs)
	}
}

func TestNoteSaveListDone(t *testing.T) {
	isolate(t)
	work := t.TempDir()
	sub := filepath.Join(work, "pkg", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	out := mustCLI(t, "remember the migration order", "note", "save", "migration order", "--cwd", work)
	if !strings.Contains(out, "N1") {
		t.Fatalf("save: %q", out)
	}
	if list := mustCLI(t, "", "note", "list", "--cwd", sub); !strings.Contains(list, "migration order") {
		t.Fatalf("list from subfolder: %q", list)
	}
	mustCLI(t, "", "note", "done", "N1", "--cwd", work)
	if list := mustCLI(t, "", "note", "list", "--cwd", sub); strings.Contains(list, "migration order") {
		t.Fatalf("after done: %q", list)
	}
	if code, _, _ := runCLI(t, "", "note", "done", "N99", "--cwd", work); code != 1 {
		t.Fatalf("unknown note: exit %d", code)
	}
}
