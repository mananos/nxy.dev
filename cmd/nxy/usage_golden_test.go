package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/mananos/nxy.dev/internal/goldentest"
	"github.com/mananos/nxy.dev/internal/host/claudecode/transcript"
	"github.com/mananos/nxy.dev/internal/jscompat"
)

// The usage-cli golden (scripts/golden/usage-cli.mjs documents the case format): the 1.x ran each
// case in a child node process; here the Go use case runs over the same world.

const defaultGoldenNow = "2026-09-10T12:00:00Z"

type usageCase struct {
	Name  string `json:"name"`
	Input struct {
		TZ            string                     `json:"tz"`
		Now           string                     `json:"now"`
		Session       string                     `json:"session"`
		Args          []string                   `json:"args"`
		RootSpace     bool                       `json:"rootSpace"`
		Files         map[string]json.RawMessage `json:"files"`
		Steps         []statusStep               `json:"steps"`
		UserConfig    json.RawMessage            `json:"userConfig"`
		ProjectConfig json.RawMessage            `json:"projectConfig"`
		Git           json.RawMessage            `json:"git"`
		FilterRows    []json.RawMessage          `json:"filterRows"`
		Rtk           *struct {
			Gain     json.RawMessage `json:"gain"`
			GainText *string         `json:"gainText"`
		} `json:"rtk"`
		Ledger *struct {
			Rows    json.RawMessage `json:"rows"`
			Garbage bool            `json:"garbage"`
			NoTable bool            `json:"noTable"`
		} `json:"ledger"`
	} `json:"input"`
	Output struct {
		Stdout string `json:"stdout"`
		Stderr string `json:"stderr"`
		Code   int    `json:"code"`
	} `json:"output"`
}

// statusStep is one statusline run: files written or appended first, then the stdin payload
// (a string is the text itself, anything else is JSON.stringify of it).
type statusStep struct {
	Stdin  json.RawMessage            `json:"stdin"`
	Write  map[string]json.RawMessage `json:"write"`
	Append map[string]json.RawMessage `json:"append"`
}

func readGoldenFile(t *testing.T, name string, v any) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(goldentest.Root(t), "testdata", "golden", "usage-cli", name))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
}

func loadUsageTexts(t *testing.T) map[string]string {
	t.Helper()
	var all []struct {
		Name   string `json:"name"`
		Output struct {
			Text string `json:"text"`
		} `json:"output"`
	}
	readGoldenFile(t, "texts.json", &all)
	out := make(map[string]string, len(all))
	for _, c := range all {
		out[c.Name] = c.Output.Text
	}
	return out
}

var (
	stubOnce sync.Once
	stubPath string
	stubDir  string
	stubErr  error
)

// buildRtkStub builds scripts/golden/rtkstub once per test run (before PATH is emptied).
func buildRtkStub(t *testing.T) string {
	t.Helper()
	stubOnce.Do(func() {
		dir, err := os.MkdirTemp("", "rtkstub")
		if err != nil {
			stubErr = err
			return
		}
		stubDir = dir
		name := "rtk"
		if runtime.GOOS == "windows" {
			name = "rtk.exe"
		}
		stubPath = filepath.Join(dir, name)
		if out, err := exec.Command("go", "build", "-o", stubPath, "github.com/mananos/nxy.dev/scripts/golden/rtkstub").CombinedOutput(); err != nil {
			stubErr = fmt.Errorf("go build rtkstub: %v: %s", err, out)
		}
	})
	if stubErr != nil {
		t.Fatal(stubErr)
	}
	return stubPath
}

func TestMain(m *testing.M) {
	code := m.Run()
	if stubDir != "" {
		os.RemoveAll(stubDir)
	}
	os.Exit(code)
}

// usageWorld is a case materialized under a temp root.
type usageWorld struct {
	root, proj string
	now        time.Time
	fill       func(string) string
	getenv     func(string) string
	loc        *time.Location
}

func mustWrite(t *testing.T, path string, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

// compactJSON is JSON.stringify of the decoded value.
func compactJSON(t *testing.T, raw []byte) string {
	t.Helper()
	out, err := jscompat.Restringify(raw, "")
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// fileSpec is the object form of a files entry.
type fileSpec struct {
	Ref   *string         `json:"ref"`
	Text  *string         `json:"text"`
	JSON  json.RawMessage `json:"json"`
	Lines []int           `json:"lines"`
	Mtime string          `json:"mtime"`
}

func (w *usageWorld) specText(t *testing.T, raw json.RawMessage, texts map[string]string) (string, time.Time) {
	t.Helper()
	var spec fileSpec
	var name string
	if json.Unmarshal(raw, &name) == nil {
		spec.Ref = &name
	} else if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	var text string
	switch {
	case spec.Ref != nil:
		var ok bool
		if text, ok = texts[*spec.Ref]; !ok {
			t.Fatalf("unknown text %q", *spec.Ref)
		}
		if len(spec.Lines) == 2 {
			lines := strings.Split(text, "\n")
			from, to := spec.Lines[0], spec.Lines[1]
			if to > len(lines) {
				to = len(lines)
			}
			text = ""
			for _, l := range lines[from:to] {
				text += l + "\n"
			}
		}
	case spec.Text != nil:
		text = *spec.Text
	default:
		text = compactJSON(t, spec.JSON)
	}
	mtime := w.now
	if spec.Mtime != "" {
		ms, ok := jscompat.ParseDateIn(spec.Mtime, time.UTC)
		if !ok {
			t.Fatalf("bad mtime %q", spec.Mtime)
		}
		mtime = time.UnixMilli(ms)
	}
	return w.fill(text), mtime
}

func nonAlnumToDash(s string) string {
	return regexp.MustCompile(`[^A-Za-z0-9]`).ReplaceAllString(s, "-")
}

func materializeUsage(t *testing.T, c *usageCase, texts map[string]string) *usageWorld {
	t.Helper()
	prefix := "nxy-golden-"
	if c.Input.RootSpace {
		prefix = "nxy golden "
	}
	dir, err := os.MkdirTemp("", prefix)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	escRoot, _ := json.Marshal(root)
	esc := string(escRoot[1 : len(escRoot)-1])
	w := &usageWorld{root: root, proj: filepath.Join(root, "proj")}
	w.fill = func(s string) string {
		return strings.ReplaceAll(strings.ReplaceAll(s, "@ROOT@", esc), "@ROOTRAW@", root)
	}
	nowText := c.Input.Now
	if nowText == "" {
		nowText = defaultGoldenNow
	}
	nowMs, ok := jscompat.ParseDateIn(nowText, time.UTC)
	if !ok {
		t.Fatalf("bad now %q", nowText)
	}
	w.now = time.UnixMilli(nowMs)
	tz := c.Input.TZ
	if tz == "" {
		tz = "UTC"
	}
	if w.loc, err = time.LoadLocation(tz); err != nil {
		t.Fatal(err)
	}

	nxyHome := filepath.Join(root, "nxyhome")
	dbPath := filepath.Join(root, "rtk", "history.db")
	if err := os.MkdirAll(filepath.Join(w.proj, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if g := strings.TrimSpace(string(c.Input.Git)); g != "false" {
		gitText := "ref: refs/heads/main\n"
		if g != "" && g != "null" {
			if err := json.Unmarshal(c.Input.Git, &gitText); err != nil {
				t.Fatal(err)
			}
		}
		mustWrite(t, filepath.Join(w.proj, ".git", "HEAD"), gitText)
	}

	slug := nonAlnumToDash(w.proj)
	for key, raw := range c.Input.Files {
		path := filepath.Join(root, filepath.FromSlash(strings.ReplaceAll(key, "@SLUG@", slug)))
		text, mtime := w.specText(t, raw, texts)
		mustWrite(t, path, text)
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	if len(c.Input.UserConfig) > 0 && string(c.Input.UserConfig) != "null" {
		mustWrite(t, filepath.Join(nxyHome, "config.json"), w.fill(compactJSON(t, c.Input.UserConfig)))
	}
	mustWrite(t, filepath.Join(w.proj, ".nxy", "config.json"), w.fill(compactJSON(t, c.Input.ProjectConfig)))
	if c.Input.FilterRows != nil {
		var lines []string
		for _, r := range c.Input.FilterRows {
			var s string
			if json.Unmarshal(r, &s) == nil {
				lines = append(lines, s)
			} else {
				lines = append(lines, compactJSON(t, r))
			}
		}
		mustWrite(t, filepath.Join(w.proj, ".nxy", "local", "metrics", "filter.jsonl"), strings.Join(lines, "\n")+"\n")
	}
	if l := c.Input.Ledger; l != nil {
		writeLedger(t, dbPath, l.Garbage, l.NoTable, w.fill(string(l.Rows)))
	}

	// No rtk anywhere unless the case has one: an empty PATH, and the stub through NXY_RTK_PATH.
	emptyPath := filepath.Join(root, "emptybin")
	if err := os.MkdirAll(emptyPath, 0o755); err != nil {
		t.Fatal(err)
	}
	rtkPath := ""
	if c.Input.Rtk != nil {
		rtkPath = buildRtkStub(t)
	}
	env := map[string]string{
		"HOME": filepath.Join(root, "home"), "USERPROFILE": filepath.Join(root, "home"),
		"APPDATA": filepath.Join(root, "appdata"), "LOCALAPPDATA": filepath.Join(root, "localappdata"),
		"XDG_DATA_HOME": filepath.Join(root, "xdgdata"), "XDG_CONFIG_HOME": filepath.Join(root, "xdgconfig"),
		"NXY_HOME": nxyHome, "RTK_DB_PATH": dbPath, "TZ": tz, "PATH": emptyPath,
		"NXY_RTK_PATH": rtkPath, "NXY_GOLDEN_RTK_GAIN": "",
	}
	if r := c.Input.Rtk; r != nil {
		// gain: null (and absent) leaves NXY_GOLDEN_RTK_GAIN unset, so the stub exits 1.
		gainPath := filepath.Join(root, "gain.json")
		switch {
		case r.GainText != nil:
			mustWrite(t, gainPath, *r.GainText)
			env["NXY_GOLDEN_RTK_GAIN"] = gainPath
		case len(r.Gain) > 0 && string(r.Gain) != "null":
			mustWrite(t, gainPath, compactJSON(t, r.Gain))
			env["NXY_GOLDEN_RTK_GAIN"] = gainPath
		}
	}
	if c.Input.Session != "" {
		env["CLAUDE_CODE_SESSION_ID"] = c.Input.Session
	}
	for k, v := range env {
		t.Setenv(k, v) // platform.UserDir reads NXY_HOME from the process
	}
	w.getenv = func(k string) string { return env[k] }
	return w
}

func writeLedger(t *testing.T, dbPath string, garbage, noTable bool, rowsJSON string) {
	t.Helper()
	if garbage {
		mustWrite(t, dbPath, strings.Repeat("this is not a sqlite database\n", 40))
		return
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	exec := func(q string, args ...any) {
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	if noTable {
		exec(`CREATE TABLE other (id INTEGER PRIMARY KEY, note TEXT)`)
		return
	}
	exec(`CREATE TABLE commands (id INTEGER PRIMARY KEY AUTOINCREMENT, timestamp TEXT NOT NULL, project_path TEXT, input_tokens INTEGER, output_tokens INTEGER, saved_tokens INTEGER)`)
	var rows []map[string]any
	if err := json.Unmarshal([]byte(rowsJSON), &rows); err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		exec(`INSERT INTO commands (timestamp, project_path, input_tokens, output_tokens, saved_tokens) VALUES (?, ?, ?, ?, ?)`,
			r["timestamp"], r["project_path"], r["input_tokens"], r["output_tokens"], r["saved_tokens"])
	}
}

// expandArgs replaces @ROOT@ (the plain root) in argv.
func (w *usageWorld) expandArgs(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = strings.ReplaceAll(a, "@ROOT@", w.root)
	}
	return out
}

// diffAt shows both texts around the first byte where they differ.
func diffAt(want, got string) string {
	i := 0
	for i < len(want) && i < len(got) && want[i] == got[i] {
		i++
	}
	lo := i - 60
	if lo < 0 {
		lo = 0
	}
	cut := func(s string) string {
		hi := i + 60
		if hi > len(s) {
			hi = len(s)
		}
		if lo > hi {
			return ""
		}
		return s[lo:hi]
	}
	return "first difference at byte " + strconv.Itoa(i) + "\n want: ..." + strconv.Quote(cut(want)) + "\n  got: ..." + strconv.Quote(cut(got))
}

func checkUsageOutput(t *testing.T, c *usageCase, w *usageWorld, stdout, stderr string, code int) {
	t.Helper()
	if got := goldentest.NormRootText(stdout, w.root); got != c.Output.Stdout {
		t.Errorf("stdout differs: %s", diffAt(c.Output.Stdout, got))
	}
	if got := goldentest.NormRootText(stderr, w.root); got != c.Output.Stderr {
		t.Errorf("stderr differs: %s", diffAt(c.Output.Stderr, got))
	}
	if code != c.Output.Code {
		t.Errorf("exit code %d, want %d", code, c.Output.Code)
	}
}

func TestGoldenStats(t *testing.T) {
	texts := loadUsageTexts(t)
	var cases []usageCase
	readGoldenFile(t, "stats.json", &cases)
	if len(cases) == 0 {
		t.Fatal("no stats cases")
	}
	for i := range cases {
		c := &cases[i]
		t.Run(c.Name, func(t *testing.T) {
			w := materializeUsage(t, c, texts)
			u := newUsage(w.getenv, func() string { return w.proj }, func() time.Time { return w.now }, w.loc)
			out := u.Stats(w.expandArgs(c.Input.Args))
			checkUsageOutput(t, c, w, out.Stdout, out.Stderr, out.Code)
		})
	}
}

func TestGoldenTrend(t *testing.T) {
	texts := loadUsageTexts(t)
	var cases []usageCase
	readGoldenFile(t, "trend.json", &cases)
	if len(cases) == 0 {
		t.Fatal("no trend cases")
	}
	for i := range cases {
		c := &cases[i]
		t.Run(c.Name, func(t *testing.T) {
			w := materializeUsage(t, c, texts)
			u := newUsage(w.getenv, func() string { return w.proj }, func() time.Time { return w.now }, w.loc)
			out := u.Trend(w.expandArgs(c.Input.Args))
			checkUsageOutput(t, c, w, out.Stdout, out.Stderr, out.Code)
		})
	}
}

// writeSpecs creates (or appends to) the files of a statusline step.
func (w *usageWorld) writeSpecs(t *testing.T, files map[string]json.RawMessage, texts map[string]string, appendTo bool) {
	t.Helper()
	slug := nonAlnumToDash(w.proj)
	for key, raw := range files {
		path := filepath.Join(w.root, filepath.FromSlash(strings.ReplaceAll(key, "@SLUG@", slug)))
		text, mtime := w.specText(t, raw, texts)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
		if appendTo {
			flags = os.O_WRONLY | os.O_CREATE | os.O_APPEND
		}
		f, err := os.OpenFile(path, flags, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.WriteString(text); err != nil {
			t.Fatal(err)
		}
		f.Close()
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
}

var offsetRe = regexp.MustCompile(`"offset":\d+`)

func TestGoldenStatusline(t *testing.T) {
	texts := loadUsageTexts(t)
	var raws []json.RawMessage
	readGoldenFile(t, "statusline.json", &raws)
	if len(raws) == 0 {
		t.Fatal("no statusline cases")
	}
	for _, raw := range raws {
		var c usageCase
		var golden struct {
			Output struct {
				Steps []struct {
					Stdout string `json:"stdout"`
					Stderr string `json:"stderr"`
					Code   int    `json:"code"`
				} `json:"steps"`
				State map[string]string `json:"state"`
				Input *string           `json:"input"`
			} `json:"output"`
		}
		if err := json.Unmarshal(raw, &c); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &golden); err != nil {
			t.Fatal(err)
		}
		t.Run(c.Name, func(t *testing.T) {
			w := materializeUsage(t, &c, texts)
			sl := newStatusline(w.getenv, func() string { return w.proj }, func() time.Time { return w.now })
			if len(c.Input.Steps) != len(golden.Output.Steps) {
				t.Fatalf("%d steps, golden has %d", len(c.Input.Steps), len(golden.Output.Steps))
			}
			for i, s := range c.Input.Steps {
				w.writeSpecs(t, s.Write, texts, false)
				w.writeSpecs(t, s.Append, texts, true)
				var stdin string
				if json.Unmarshal(s.Stdin, &stdin) != nil {
					stdin = compactJSON(t, s.Stdin)
				}
				out := sl.Run([]byte(w.fill(stdin)))
				want := golden.Output.Steps[i]
				if got := goldentest.NormRootText(out.Stdout, w.root); got != want.Stdout {
					t.Errorf("step %d stdout differs: %s", i, diffAt(want.Stdout, got))
				}
				if got := goldentest.NormRootText(out.Stderr, w.root); got != want.Stderr {
					t.Errorf("step %d stderr differs: %s", i, diffAt(want.Stderr, got))
				}
				if out.Code != want.Code {
					t.Errorf("step %d exit code %d, want %d", i, out.Code, want.Code)
				}
			}
			cache := filepath.Join(w.root, "nxyhome", "cache")
			state := map[string]string{}
			sizes := map[int64]bool{}
			_ = filepath.WalkDir(w.root, func(p string, d fs.DirEntry, err error) error {
				if err == nil && !d.IsDir() && strings.HasSuffix(p, ".jsonl") {
					if info, err := d.Info(); err == nil {
						sizes[info.Size()] = true
					}
				}
				return nil
			})
			if entries, err := os.ReadDir(filepath.Join(cache, "statusline-v2")); err == nil {
				for _, e := range entries {
					b, err := os.ReadFile(filepath.Join(cache, "statusline-v2", e.Name()))
					if err != nil {
						t.Fatal(err)
					}
					// offsets count bytes of transcripts that embed the root path (the golden masks them),
					// so first check each one is the byte size of a real transcript
					for _, m := range offsetRe.FindAllString(string(b), -1) {
						n, _ := strconv.ParseInt(strings.TrimPrefix(m, `"offset":`), 10, 64)
						if n != 0 && !sizes[n] {
							t.Errorf("state %s: offset %d is not the size of any transcript", e.Name(), n)
						}
					}
					state[strings.TrimSuffix(e.Name(), ".json")] = offsetRe.ReplaceAllString(goldentest.NormRootText(string(b), w.root), `"offset":0`)
				}
			}
			if len(state) != len(golden.Output.State) {
				t.Errorf("state files %d, golden has %d", len(state), len(golden.Output.State))
			}
			for k, want := range golden.Output.State {
				if got, ok := state[k]; !ok {
					t.Errorf("state %q missing", k)
				} else if got != want {
					t.Errorf("state %q differs: %s", k, diffAt(want, got))
				}
			}
			var gotInput *string
			if b, err := os.ReadFile(filepath.Join(cache, "statusline-v2-input.json")); err == nil {
				s := goldentest.NormRootText(string(b), w.root)
				gotInput = &s
			}
			switch {
			case gotInput == nil && golden.Output.Input != nil, gotInput != nil && golden.Output.Input == nil:
				t.Errorf("input file presence differs (got %v, want %v)", gotInput != nil, golden.Output.Input != nil)
			case gotInput != nil && *gotInput != *golden.Output.Input:
				t.Errorf("input file differs: %s", diffAt(*golden.Output.Input, *gotInput))
			}
			// the 1.x files are never written
			if entries, err := os.ReadDir(cache); err == nil {
				for _, e := range entries {
					if strings.HasPrefix(e.Name(), "statusline-") && !strings.HasPrefix(e.Name(), "statusline-v2") {
						t.Errorf("1.x file %s written", e.Name())
					}
				}
			}
		})
	}
}

func TestStatuslineStateCleanup(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	s := statusState{dir: dir, clock: func() time.Time { return now }}
	touch := func(name string, age time.Duration) string {
		p := filepath.Join(s.sessionDir(), name)
		mustWrite(t, p, "{}")
		mt := now.Add(-age)
		if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatal(err)
		}
		return p
	}
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }
	old := touch("old.json", 8*24*time.Hour)
	recent := touch("recent.json", 6*24*time.Hour)

	if err := s.Save("new", transcript.NewSessionCache()); err != nil {
		t.Fatal(err)
	}
	if exists(old) {
		t.Error("an 8-day-old state file survived the creation of a new session state")
	}
	if !exists(recent) {
		t.Error("a 6-day-old state file was removed")
	}
	if !exists(filepath.Join(s.sessionDir(), "new.json")) {
		t.Error("the new state file is missing")
	}

	// a later render of the same session removes nothing
	old2 := touch("old2.json", 9*24*time.Hour)
	if err := s.Save("new", transcript.NewSessionCache()); err != nil {
		t.Fatal(err)
	}
	if !exists(old2) {
		t.Error("cleanup ran on a render of an existing session")
	}
}

func TestStateFileName(t *testing.T) {
	for in, want := range map[string]string{
		"abc-DEF_09": "abc-DEF_09.json",
		"a/b c":      "a_b_c.json",
		"../x":       "___x.json",
		"é":          "_.json",
		"\U0001F600": "__.json", // two UTF-16 units, as in JS
		"":           ".json",
	} {
		if got := stateFileName(in); got != want {
			t.Errorf("stateFileName(%q) = %q, want %q", in, got, want)
		}
	}
}
