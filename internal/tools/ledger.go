package tools

import (
	"database/sql"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/mananos/nxy.dev/internal/jscompat"
	"github.com/mananos/nxy.dev/internal/report"

	_ "modernc.org/sqlite"
)

// RtkHistoryDBPath is RTK_DB_PATH when set (rtk honours it too), else history.db next to the
// warning marker (core/filter/ledger.mjs rtkHistoryDbPath).
func RtkHistoryDBPath(goos string, getenv func(string) string) string {
	if p := getenv("RTK_DB_PATH"); p != "" {
		return p
	}
	return filepath.Join(filepath.Dir(warnMarkerPath(goos, getenv)), "history.db")
}

// LedgerUnavailable says why ReadLedger would have nothing: "no-db" when the file is missing, ""
// otherwise. The 1.x "no-sqlite" cannot happen here: sqlite is built in.
func LedgerUnavailable(dbPath string) string {
	if _, err := os.Stat(dbPath); err != nil {
		return "no-db"
	}
	return ""
}

// num is JS `Number(v) || 0` over a value the sqlite driver returned.
func num(v any) float64 {
	var f float64
	switch x := v.(type) {
	case int64:
		f = float64(x)
	case float64:
		f = x
	case string:
		f = jscompat.ToNumber(x)
	case []byte:
		f = jscompat.ToNumber(string(x))
	case bool:
		if x {
			f = 1
		}
	}
	if math.IsNaN(f) {
		return 0
	}
	return f
}

func text(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case []byte:
		return string(x)
	case string:
		return x
	default:
		return fmt.Sprint(x)
	}
}

var uriEscaper = strings.NewReplacer("%", "%25", "?", "%3F", "#", "%23", " ", "%20")

// readOnlyURI builds the sqlite URI by hand: an empty authority ("file://") followed by the
// escaped path, so a UNC path (//server/share/x.db) keeps its leading "//" and a space, "?" or "#"
// cannot end up in the URI syntax.
func readOnlyURI(dbPath string) string {
	slashed := filepath.ToSlash(dbPath)
	if !strings.HasPrefix(slashed, "/") {
		slashed = "/" + slashed
	}
	return "file://" + uriEscaper.Replace(slashed) + "?mode=ro"
}

func isoSecond(ms int64) string {
	return time.UnixMilli(ms).UTC().Format("2006-01-02T15:04:05")
}

// ReadLedger returns the rows of rtk's ledger in [since, until] (epoch ms; nil or 0 is open),
// read-only. A missing file, one that will not open and a failed query give false.
func ReadLedger(dbPath string, since, until *int64) ([]report.LedgerRow, bool) {
	if dbPath == "" {
		return nil, false
	}
	if _, err := os.Stat(dbPath); err != nil {
		return nil, false
	}
	db, err := sql.Open("sqlite", readOnlyURI(dbPath))
	if err != nil {
		return nil, false
	}
	defer db.Close()
	lo, hi := "", "9"
	if since != nil && *since != 0 {
		lo = isoSecond(*since - 1000)
	}
	if until != nil && *until != 0 {
		hi = isoSecond(*until+1000) + "Z"
	}
	rs, err := db.Query(`SELECT timestamp, project_path, input_tokens, output_tokens, saved_tokens FROM commands WHERE timestamp >= ? AND timestamp <= ? ORDER BY timestamp`, lo, hi)
	if err != nil {
		return nil, false
	}
	defer rs.Close()
	out := []report.LedgerRow{}
	for rs.Next() {
		var ts, project, in, outTok, saved any
		if rs.Scan(&ts, &project, &in, &outTok, &saved) != nil {
			return nil, false
		}
		ms, ok := jscompat.ParseDateIn(text(ts), time.UTC)
		if !ok {
			continue
		}
		if since != nil && *since != 0 && ms < *since {
			continue
		}
		if until != nil && *until != 0 && ms > *until {
			continue
		}
		out = append(out, report.LedgerRow{
			Ts:      ms,
			Project: report.NormalizeProjectPath(text(project), runtime.GOOS),
			Input:   num(in), Output: num(outTok), Saved: num(saved),
		})
	}
	if rs.Err() != nil {
		return nil, false
	}
	return out, true
}
