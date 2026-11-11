package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mananos/nxy.dev/internal/app"
	"github.com/mananos/nxy.dev/internal/legacy"
	"github.com/mananos/nxy.dev/internal/platform"
	"github.com/mananos/nxy.dev/internal/report"
	"github.com/mananos/nxy.dev/internal/store/sqlite"
)

const legacyUsage = `usage: nxy legacy <import [--dir <d>] (copy what the 1.x knows of the repo; what was already imported says so) | status (what was imported, per repo and checkout)>`

// legacyFiles reads what the 1.x left on disk: memory.db on a copy, the jsonl files as they are.
type legacyFiles struct{}

var _ app.LegacyData = legacyFiles{}

func (legacyFiles) ProjectKey(dir string) string { return platform.LegacyProjectKey(dir) }
func (legacyFiles) RepoKey(dir string) string    { return platform.RepoKey(dir) }
func (legacyFiles) Checkout(dir string) string   { return platform.FilterCheckout(dir) }

func (legacyFiles) Folder(dir string) string {
	dir = platform.NativePath(dir)
	if root, ok := platform.GitRoot(dir); ok {
		return root
	}
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return dir
}

func legacyMemoryPath() string {
	p, err := platform.LegacyMemoryDB(os.Getenv)
	if err != nil {
		return ""
	}
	return p
}

func (legacyFiles) HasMemory() bool {
	p := legacyMemoryPath()
	if p == "" {
		return false
	}
	_, ok := platform.FileSize(p)
	return ok
}

func (legacyFiles) Memory(ctx context.Context, project string) ([]legacy.Memory, []legacy.Edge, error) {
	return sqlite.ReadLegacyMemory(ctx, legacyMemoryPath(), project)
}

func (legacyFiles) HasReview(dir string) bool {
	_, ok := platform.FileSize(platform.LegacyReviewLog(dir))
	return ok
}

func (legacyFiles) Review(dir string) []any { return platform.ReadJSONL(platform.LegacyReviewLog(dir)) }

func (legacyFiles) FilterSize(dir string) (int64, bool) {
	return platform.FileSize(platform.LegacyFilterLog(dir))
}

func (legacyFiles) FilterHead(dir string, upTo int64) string {
	return platform.FileHead(platform.LegacyFilterLog(dir), upTo)
}

func (legacyFiles) FilterFrom(dir string, offset int64) ([]any, legacy.Cursor, error) {
	return platform.ReadJSONLFrom(platform.LegacyFilterLog(dir), offset)
}

// lazyStore meets app.LegacyStore.
var _ app.LegacyStore = (*lazyStore)(nil)

func (l *lazyStore) ImportLegacyMemory(ctx context.Context, repo string, imp legacy.Import) (legacy.Counts, bool, error) {
	s, err := l.open(ctx)
	if err != nil {
		return legacy.Counts{}, false, err
	}
	return s.ImportLegacyMemory(ctx, repo, imp)
}

func (l *lazyStore) ImportLegacyReviews(ctx context.Context, checkout string, picks []legacy.Pick, c legacy.Counts) (bool, error) {
	s, err := l.open(ctx)
	if err != nil {
		return false, err
	}
	return s.ImportLegacyReviews(ctx, checkout, picks, c)
}

func (l *lazyStore) ImportLegacyFilter(ctx context.Context, checkout string, from, to legacy.Cursor, groups map[string][]report.FilterGroup, reset bool) (legacy.FilterResult, error) {
	s, err := l.open(ctx)
	if err != nil {
		return legacy.FilterResult{}, err
	}
	return s.ImportLegacyFilter(ctx, checkout, from, to, groups, reset)
}

func (l *lazyStore) LegacyImports(ctx context.Context) ([]legacy.Mark, []legacy.StoredCursor, error) {
	s, err := l.open(ctx)
	if err != nil {
		return nil, nil, err
	}
	return s.LegacyImports(ctx)
}

func (l *lazyStore) SetLegacyError(ctx context.Context, repo string, at int64, msg string) error {
	s, err := l.open(ctx)
	if err != nil {
		return err
	}
	return s.SetLegacyError(ctx, repo, at, msg)
}

func (l *lazyStore) AddFilterGroup(ctx context.Context, checkout, session string, g report.FilterGroup) error {
	// The PostToolUse hook runs on every Bash call: it must not wait on a busy db.
	s, err := l.openQuick(ctx)
	if err != nil {
		return err
	}
	return s.AddFilterGroup(ctx, checkout, session, g)
}

// runLegacy is `nxy legacy`: run the 1.x import by hand and see what has been imported.
func runLegacy(args []string, _ io.Reader, stdout, stderr io.Writer) (code int) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(stderr, "nxy legacy: %v\n", r)
			code = 1
		}
	}()
	if len(args) == 0 {
		fmt.Fprintln(stderr, legacyUsage)
		return 1
	}
	opts, _ := parseLocateArgs(args[1:])
	dir := opts["dir"]
	if dir == "true" {
		dir = ""
	}
	dir = resolveCwd(setupArgs{cwd: dir})
	ctx := context.Background()
	switch args[0] {
	case "import":
		return legacyImport(ctx, dir, stdout, stderr)
	case "status":
		return legacyStatus(ctx, stdout, stderr)
	}
	fmt.Fprintln(stderr, legacyUsage)
	return 1
}

func legacyDate(ms int64) string { return time.UnixMilli(ms).Format("2006-01-02") }

func legacyImport(ctx context.Context, dir string, stdout, stderr io.Writer) int {
	h, st := newHooksStore()
	defer st.Close()
	d := legacyFiles{}
	if !d.HasMemory() && !d.HasReview(dir) {
		if _, ok := d.FilterSize(dir); !ok {
			fmt.Fprintf(stdout, "nada que importar: la 1.x no dejó memoria ni archivos de review o filtro para %s\n", dir)
			return 0
		}
	}
	before, _, err := st.LegacyImports(ctx)
	if err != nil {
		return memFail(stderr, err)
	}
	was := map[string]bool{}
	for _, m := range before {
		was[m.Scope+"\x00"+m.Key] = true
	}
	counts, _, ierr := h.ImportLegacyNow(ctx, dir)
	after, _, err := st.LegacyImports(ctx)
	if err != nil {
		return memFail(stderr, err)
	}
	repo, checkout := d.RepoKey(dir), d.Checkout(dir)
	labels := map[string]string{sqlite.LegacyScopeMemory: "memoria", sqlite.LegacyScopeReview: "review", sqlite.LegacyScopeGlobal: "memoria global"}
	for _, m := range after {
		if !(m.Scope == sqlite.LegacyScopeMemory && m.Key == repo) && !(m.Scope == sqlite.LegacyScopeReview && m.Key == checkout) &&
			m.Scope != sqlite.LegacyScopeGlobal {
			continue
		}
		if was[m.Scope+"\x00"+m.Key] {
			fmt.Fprintf(stdout, "%s: ya importado el %s — %s\n", labels[m.Scope], legacyDate(m.ImportedAt), orNothing(m.Counts.Render()))
		} else {
			fmt.Fprintf(stdout, "%s: importado ahora — %s\n", labels[m.Scope], orNothing(m.Counts.Render()))
		}
	}
	if counts.FilterRows > 0 || counts.FilterSessions > 0 {
		fmt.Fprintf(stdout, "filtro: %d filas nuevas de %d sesiones", counts.FilterRows, counts.FilterSessions)
		if counts.FilterV2 > 0 {
			fmt.Fprintf(stdout, " (%d sesiones ya escritas por la v2, sin tocar)", counts.FilterV2)
		}
		if counts.FilterNoSess > 0 {
			fmt.Fprintf(stdout, " (%d filas sin sesión, no entran)", counts.FilterNoSess)
		}
		fmt.Fprintln(stdout)
	} else if _, ok := d.FilterSize(dir); ok {
		fmt.Fprintln(stdout, "filtro: sin filas nuevas")
	}
	if ierr != nil {
		fmt.Fprintf(stderr, "nxy: no se pudo importar todo: %v\n", ierr)
		return 1
	}
	return 0
}

func orNothing(s string) string {
	if s == "" {
		return "sin datos"
	}
	return s
}

func legacyStatus(ctx context.Context, stdout, stderr io.Writer) int {
	path, err := platform.DBPath()
	if err != nil {
		return memFail(stderr, err)
	}
	if _, ok := platform.FileSize(path); !ok {
		fmt.Fprintln(stdout, "nada importado de la 1.x todavía")
		return 0
	}
	_, st := newHooksStore()
	defer st.Close()
	marks, cursors, err := st.LegacyImports(ctx)
	if err != nil {
		return memFail(stderr, err)
	}
	if len(marks) == 0 && len(cursors) == 0 {
		fmt.Fprintln(stdout, "nada importado de la 1.x todavía")
		return 0
	}
	for _, m := range marks {
		if m.Scope == legacy.ScopeMemoryError {
			fmt.Fprintf(stdout, "memoria de %s — último error el %s: %s (`nxy legacy import` lo reintenta)\n", m.Key, legacyDate(m.ImportedAt), m.Error)
			continue
		}
		if m.Scope == sqlite.LegacyScopeGlobal {
			fmt.Fprintf(stdout, "memoria global — importado el %s — %s\n", legacyDate(m.ImportedAt), orNothing(m.Counts.Render()))
			continue
		}
		kind := "memoria de "
		if m.Scope == sqlite.LegacyScopeReview {
			kind = "review de "
		}
		fmt.Fprintf(stdout, "%s%s — importado el %s — %s\n", kind, m.Key, legacyDate(m.ImportedAt), orNothing(m.Counts.Render()))
	}
	for _, c := range cursors {
		file := filepath.Join(filepath.FromSlash(c.Checkout), "metrics", "filter.jsonl")
		of := "el archivo ya no está"
		if size, ok := platform.FileSize(file); ok {
			of = fmt.Sprintf("de %d bytes", size)
		}
		fmt.Fprintf(stdout, "filtro de %s — leídos %d bytes %s (última lectura el %s)\n",
			strings.TrimSuffix(c.Checkout, "/.nxy/local"), c.Cursor.Offset, of, legacyDate(c.UpdatedAt))
	}
	return 0
}
