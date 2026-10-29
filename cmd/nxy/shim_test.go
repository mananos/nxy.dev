package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"net/http"
	"net/http/httptest"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/matyy-a/nxy.dev/internal/version"
)

const (
	hookLine    = `. "%s"; exit $LASTEXITCODE`
	devHookLine = `. "${CLAUDE_PLUGIN_ROOT}/../../bin/nxy.cmd"; exit $LASTEXITCODE`
	hookJSON    = `{"hook_event_name":"PreToolUse","tool_name":"Bash","session_id":"s1","cwd":"."}`
)

// fakeSrc stands in for the nxy binary: it records its args and stdin in $NXY_MARK
// and exits with $NXY_FAKE_EXIT (default 0). It proves what the shim ran and that stdin got through.
const fakeSrc = `package main

import (
	"io"
	"os"
	"strconv"
	"strings"
)

func main() {
	in, _ := io.ReadAll(os.Stdin)
	if m := os.Getenv("NXY_MARK"); m != "" {
		os.WriteFile(m, []byte(strings.Join(os.Args[1:], " ")+"\n"+string(in)), 0o644)
	}
	code, _ := strconv.Atoi(os.Getenv("NXY_FAKE_EXIT"))
	os.Exit(code)
}
`

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

func exeName(base string) string {
	if runtime.GOOS == "windows" {
		return base + ".exe"
	}
	return base
}

func goBuild(t *testing.T, dir, out string, args ...string) {
	t.Helper()
	cmd := exec.Command("go", append([]string{"build", "-o", out}, args...)...)
	cmd.Dir = dir
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, b)
	}
}

func buildFake(t *testing.T) string {
	t.Helper()
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "fake.go"), []byte(fakeSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), exeName("fake"))
	goBuild(t, src, out, "fake.go")
	return out
}

type shellSpec struct {
	name string
	argv []string // program + flags, the hook line is appended
	ps   bool
}

func shells(t *testing.T) []shellSpec {
	t.Helper()
	var out []shellSpec
	for _, n := range []string{"sh", "dash", "bash"} {
		p, err := exec.LookPath(n)
		if err != nil {
			continue
		}
		// On Windows System32\bash.exe is the WSL launcher, not Git Bash.
		if runtime.GOOS == "windows" && strings.Contains(strings.ToLower(p), "system32") {
			continue
		}
		out = append(out, shellSpec{name: n, argv: []string{p, "-c"}})
	}
	if runtime.GOOS == "windows" {
		if p, err := exec.LookPath("powershell"); err == nil {
			out = append(out, shellSpec{name: "powershell", argv: []string{p, "-NoProfile", "-NonInteractive", "-Command"}, ps: true})
		}
	}
	if len(out) == 0 {
		t.Skip("no shell found")
	}
	return out
}

type result struct {
	code           int
	stdout, stderr string
}

// runShim runs the exact hook line under sh with a PreToolUse payload on stdin.
func runShim(t *testing.T, sh shellSpec, env map[string]string) result {
	t.Helper()
	shim := filepath.Join(repoRoot(t), "bin", "nxy.cmd")
	if !sh.ps {
		shim = filepath.ToSlash(shim)
	}
	line := strings.Replace(hookLine, "%s", shim, 1)
	cmd := exec.Command(sh.argv[0], append(append([]string{}, sh.argv[1:]...), line)...)
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "NXY_") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	cmd.Stdin = strings.NewReader(hookJSON)
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	err := cmd.Run()
	r := result{stdout: so.String(), stderr: se.String()}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		r.code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("%s: %v", sh.name, err)
	}
	return r
}

func home(sh shellSpec, dir string) string {
	if sh.ps {
		return filepath.FromSlash(dir)
	}
	return filepath.ToSlash(dir)
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	b, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, b, 0o755); err != nil {
		t.Fatal(err)
	}
}

func assertMark(t *testing.T, mark string) {
	t.Helper()
	b, err := os.ReadFile(mark)
	if err != nil {
		t.Fatalf("binary did not run (no mark): %v", err)
	}
	s := string(b)
	if !strings.HasPrefix(s, "hook\n") || !strings.Contains(s, `"hook_event_name":"PreToolUse"`) {
		t.Fatalf("binary ran without the hook arg or stdin: %q", s)
	}
}

func TestShimInstalledBinary(t *testing.T) {
	fake := buildFake(t)
	for _, sh := range shells(t) {
		t.Run(sh.name, func(t *testing.T) {
			h := t.TempDir()
			mark := filepath.Join(t.TempDir(), "mark")
			copyFile(t, fake, filepath.Join(h, "bin", version.Version, exeName("nxy")))
			r := runShim(t, sh, map[string]string{"NXY_HOME": home(sh, h), "NXY_MARK": mark})
			if r.code != 0 || r.stdout != "" {
				t.Fatalf("code=%d stdout=%q stderr=%q", r.code, r.stdout, r.stderr)
			}
			assertMark(t, mark)
		})
	}
}

func TestShimExitCode(t *testing.T) {
	fake := buildFake(t)
	for _, sh := range shells(t) {
		t.Run(sh.name, func(t *testing.T) {
			h := t.TempDir()
			copyFile(t, fake, filepath.Join(h, "bin", version.Version, exeName("nxy")))
			r := runShim(t, sh, map[string]string{"NXY_HOME": home(sh, h), "NXY_FAKE_EXIT": "2"})
			if r.code != 2 {
				t.Fatalf("code=%d want 2 stderr=%q", r.code, r.stderr)
			}
		})
	}
}

func TestShimRealBinary(t *testing.T) {
	real := filepath.Join(t.TempDir(), exeName("nxy"))
	goBuild(t, repoRoot(t), real, "./cmd/nxy")
	for _, sh := range shells(t) {
		t.Run(sh.name, func(t *testing.T) {
			h := t.TempDir()
			copyFile(t, real, filepath.Join(h, "bin", version.Version, exeName("nxy")))
			r := runShim(t, sh, map[string]string{"NXY_HOME": home(sh, h)})
			if r.code != 0 || r.stdout != "" {
				t.Fatalf("code=%d stdout=%q stderr=%q", r.code, r.stdout, r.stderr)
			}
		})
	}
}

func TestShimNXYBinWins(t *testing.T) {
	fake := buildFake(t)
	for _, sh := range shells(t) {
		t.Run(sh.name, func(t *testing.T) {
			h := t.TempDir()
			// A binary in bin/<version>/ that the override must beat.
			copyFile(t, fake, filepath.Join(h, "bin", version.Version, exeName("nxy")))
			override := filepath.Join(t.TempDir(), exeName("other"))
			copyFile(t, fake, override)
			mark := filepath.Join(t.TempDir(), "mark")
			r := runShim(t, sh, map[string]string{
				"NXY_HOME": home(sh, h), "NXY_BIN": home(sh, override), "NXY_MARK": mark,
			})
			if r.code != 0 || r.stdout != "" {
				t.Fatalf("code=%d stdout=%q stderr=%q", r.code, r.stdout, r.stderr)
			}
			assertMark(t, mark)
			// With NXY_BIN pointing nowhere, the versioned binary must NOT be used.
			r = runShim(t, sh, map[string]string{"NXY_HOME": home(sh, h), "NXY_BIN": home(sh, filepath.Join(h, "missing")), "NXY_MARK": mark + "2"})
			if r.code != 0 || r.stdout != "" {
				t.Fatalf("missing override: code=%d stdout=%q", r.code, r.stdout)
			}
			if _, err := os.Stat(mark + "2"); err == nil {
				t.Fatal("versioned binary ran although NXY_BIN was set")
			}
			if !strings.Contains(r.stderr, "nxy:") {
				t.Fatalf("expected one nxy: line on stderr, got %q", r.stderr)
			}
		})
	}
}

func releaseDir(t *testing.T, fake string, corrupt bool) (dir, asset string) {
	t.Helper()
	dir = t.TempDir()
	goarch := runtime.GOARCH
	asset = "nxy_" + version.Version + "_" + runtime.GOOS + "_" + goarch
	if runtime.GOOS == "windows" {
		asset += ".exe"
	}
	b, err := os.ReadFile(fake)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, asset), b, 0o755); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	hexsum := hex.EncodeToString(sum[:])
	if corrupt {
		hexsum = strings.Repeat("0", 64)
	}
	if err := os.WriteFile(filepath.Join(dir, "checksums.txt"), []byte(hexsum+"  "+asset+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, asset
}

func fileURL(dir string) string {
	d := filepath.ToSlash(dir)
	if !strings.HasPrefix(d, "/") {
		d = "/" + d
	}
	return "file://" + d
}

func TestShimDownload(t *testing.T) {
	fake := buildFake(t)
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("no curl")
	}
	for _, sh := range shells(t) {
		t.Run(sh.name, func(t *testing.T) {
			rel, _ := releaseDir(t, fake, false)
			h := t.TempDir()
			mark := filepath.Join(t.TempDir(), "mark")
			r := runShim(t, sh, map[string]string{"NXY_HOME": home(sh, h), "NXY_RELEASE_URL": fileURL(rel), "NXY_MARK": mark})
			if r.code != 0 || r.stdout != "" {
				t.Fatalf("code=%d stdout=%q stderr=%q", r.code, r.stdout, r.stderr)
			}
			if _, err := os.Stat(filepath.Join(h, "bin", version.Version, exeName("nxy"))); err != nil {
				t.Fatalf("not installed: %v (stderr %q)", err, r.stderr)
			}
			assertMark(t, mark)
			left, _ := filepath.Glob(filepath.Join(h, "bin", version.Version, "*"))
			if len(left) != 1 {
				t.Fatalf("leftovers in bin dir: %v", left)
			}
		})
	}
}

func TestShimBadChecksum(t *testing.T) {
	fake := buildFake(t)
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("no curl")
	}
	for _, sh := range shells(t) {
		t.Run(sh.name, func(t *testing.T) {
			rel, _ := releaseDir(t, fake, true)
			h := t.TempDir()
			mark := filepath.Join(t.TempDir(), "mark")
			r := runShim(t, sh, map[string]string{"NXY_HOME": home(sh, h), "NXY_RELEASE_URL": fileURL(rel), "NXY_MARK": mark})
			if r.code != 0 || r.stdout != "" {
				t.Fatalf("code=%d stdout=%q", r.code, r.stdout)
			}
			if !strings.Contains(r.stderr, "checksum") {
				t.Fatalf("stderr does not name the checksum: %q", r.stderr)
			}
			if _, err := os.Stat(filepath.Join(h, "bin", version.Version, exeName("nxy"))); err == nil {
				t.Fatal("binary installed despite a bad checksum")
			}
			if _, err := os.Stat(mark); err == nil {
				t.Fatal("binary ran despite a bad checksum")
			}
		})
	}
}

func TestShimConcurrentDownload(t *testing.T) {
	fake := buildFake(t)
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("no curl")
	}
	for _, sh := range shells(t) {
		t.Run(sh.name, func(t *testing.T) {
			rel, asset := releaseDir(t, fake, false)
			var hits int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/"+asset) {
					atomic.AddInt32(&hits, 1)
					time.Sleep(2 * time.Second) // a slow download, so the others overlap it
				}
				http.FileServer(http.Dir(rel)).ServeHTTP(w, r)
			}))
			defer srv.Close()
			h := t.TempDir()
			env := map[string]string{"NXY_HOME": home(sh, h), "NXY_RELEASE_URL": srv.URL}
			const n = 4
			res := make([]result, n)
			var wg sync.WaitGroup
			for i := 0; i < n; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					res[i] = runShim(t, sh, env)
				}(i)
			}
			wg.Wait()
			for i, r := range res {
				if r.code != 0 || r.stdout != "" {
					t.Fatalf("hook %d: code=%d stdout=%q stderr=%q", i, r.code, r.stdout, r.stderr)
				}
			}
			if got := atomic.LoadInt32(&hits); got != 1 {
				t.Fatalf("binary fetched %d times, want 1", got)
			}
			if _, err := os.Stat(filepath.Join(h, "bin", version.Version, exeName("nxy"))); err != nil {
				t.Fatalf("not installed: %v", err)
			}
			left, _ := filepath.Glob(filepath.Join(h, "bin", version.Version, "*"))
			if len(left) != 1 {
				t.Fatalf("leftovers in bin dir: %v", left)
			}
		})
	}
}

func TestShimStaleCleanup(t *testing.T) {
	fake := buildFake(t)
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("no curl")
	}
	for _, sh := range shells(t) {
		t.Run(sh.name, func(t *testing.T) {
			rel, _ := releaseDir(t, fake, false)
			h := t.TempDir()
			dir := filepath.Join(h, "bin", version.Version)
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			old := time.Now().Add(-72 * time.Hour)
			stale := []string{filepath.Join(dir, exeName("nxy")+".111.tmp"), filepath.Join(dir, exeName("nxy")+".111.sums")}
			for _, p := range stale {
				if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(p, old, old); err != nil {
					t.Fatal(err)
				}
			}
			lock := filepath.Join(dir, exeName("nxy")+".lock")
			if err := os.Mkdir(lock, 0o755); err != nil {
				t.Fatal(err)
			}
			dead := time.Now().Add(-10 * time.Minute)
			if err := os.Chtimes(lock, dead, dead); err != nil {
				t.Fatal(err)
			}
			r := runShim(t, sh, map[string]string{"NXY_HOME": home(sh, h), "NXY_RELEASE_URL": fileURL(rel)})
			if r.code != 0 || r.stdout != "" {
				t.Fatalf("code=%d stdout=%q stderr=%q", r.code, r.stdout, r.stderr)
			}
			left, _ := filepath.Glob(filepath.Join(dir, "*"))
			if len(left) != 1 || filepath.Base(left[0]) != exeName("nxy") {
				t.Fatalf("stale files not cleaned, left: %v (stderr %q)", left, r.stderr)
			}
		})
	}
}

func TestShimFreshLockSkips(t *testing.T) {
	fake := buildFake(t)
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("no curl")
	}
	for _, sh := range shells(t) {
		t.Run(sh.name, func(t *testing.T) {
			rel, _ := releaseDir(t, fake, false)
			h := t.TempDir()
			dir := filepath.Join(h, "bin", version.Version)
			lock := filepath.Join(dir, exeName("nxy")+".lock")
			if err := os.MkdirAll(lock, 0o755); err != nil {
				t.Fatal(err)
			}
			mark := filepath.Join(t.TempDir(), "mark")
			r := runShim(t, sh, map[string]string{"NXY_HOME": home(sh, h), "NXY_RELEASE_URL": fileURL(rel), "NXY_MARK": mark})
			if r.code != 0 || r.stdout != "" || !strings.Contains(r.stderr, "nxy:") {
				t.Fatalf("code=%d stdout=%q stderr=%q", r.code, r.stdout, r.stderr)
			}
			if _, err := os.Stat(filepath.Join(dir, exeName("nxy"))); err == nil {
				t.Fatal("downloaded despite a fresh lock")
			}
			if _, err := os.Stat(lock); err != nil {
				t.Fatal("the lock of another run was removed")
			}
			if _, err := os.Stat(mark); err == nil {
				t.Fatal("binary ran without being installed")
			}
		})
	}
}

func TestShimVersion(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "bin", "nxy.cmd"))
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`(?m)^(?:set "NXY_VERSION=([^"]+)"|NXY_VERSION=(\S+))$`)
	m := re.FindAllStringSubmatch(string(b), -1)
	if len(m) != 2 {
		t.Fatalf("want 2 pinned lines (batch + sh), got %d", len(m))
	}
	for _, x := range m {
		if got := x[1] + x[2]; got != version.Version {
			t.Fatalf("shim pins %q, version.Version is %q", got, version.Version)
		}
	}
}

type hooksFile struct {
	Hooks map[string][]struct {
		Matcher string `json:"matcher"`
		Hooks   []struct {
			Command string `json:"command"`
		} `json:"hooks"`
	} `json:"hooks"`
}

func loadHooks(t *testing.T, path string) (hooksFile, string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var h hooksFile
	if err := json.Unmarshal(b, &h); err != nil {
		t.Fatal(err)
	}
	return h, string(b)
}

func TestDevHooksManifest(t *testing.T) {
	root := repoRoot(t)
	src, _ := loadHooks(t, filepath.Join(root, "hooks", "hooks.json"))
	dev, raw := loadHooks(t, filepath.Join(root, "dev", "plugin", "hooks", "hooks.json"))
	have := map[string]bool{}
	for ev, entries := range dev.Hooks {
		for _, e := range entries {
			have[ev+"|"+e.Matcher] = true
			for _, h := range e.Hooks {
				if h.Command != devHookLine {
					t.Errorf("%s %q: command %q is not the shim line", ev, e.Matcher, h.Command)
				}
			}
		}
	}
	for ev, entries := range src.Hooks {
		for _, e := range entries {
			if !have[ev+"|"+e.Matcher] {
				t.Errorf("dev hooks miss %s with matcher %q", ev, e.Matcher)
			}
		}
	}
	for _, bad := range []string{`C:\\`, `C:/`, `/Users/`, `/home/`, `/c/`} {
		if strings.Contains(raw, bad) {
			t.Errorf("dev hooks carry a personal path (%s)", bad)
		}
	}
}
