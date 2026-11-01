package format

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mananos/nxy.dev/internal/jscompat"
	"github.com/mananos/nxy.dev/internal/metrics"
)

// finite is Number.isFinite for a value where JS null/undefined arrive as NaN.
func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

func num(x float64) string { return jscompat.String(x) }

// FmtTokens: 842 · 4.2k · 412k · 1.3M · 12M. NaN -> "?".
func FmtTokens(n float64) string {
	switch {
	case !finite(n):
		return "?"
	case n < 1000:
		return num(jscompat.Round(n))
	case n < 10_000:
		return jscompat.ToFixed(n/1000, 1) + "k"
	case n < 1_000_000:
		return num(jscompat.Round(n/1000)) + "k"
	case n < 10_000_000:
		return jscompat.ToFixed(n/1_000_000, 1) + "M"
	}
	return num(jscompat.Round(n/1_000_000)) + "M"
}

// FmtUsd: $0.004 below $1, $1.23 above. NaN -> "?".
func FmtUsd(v float64) string {
	if !finite(v) {
		return "?"
	}
	if v >= 1 {
		return "$" + jscompat.ToFixed(v, 2)
	}
	return "$" + jscompat.ToFixed(v, 3)
}

// FmtUsdPartial is a USD sum that may be incomplete: `$164.20+?` when some call has no rate, `?`
// when nothing is known, `$1.20*` when estimated by family.
func FmtUsdPartial(v float64, partial, estimated bool) string {
	star := ""
	if estimated {
		star = "*"
	}
	if !partial {
		if finite(v) {
			return FmtUsd(v) + star
		}
		return FmtUsd(v)
	}
	if !finite(v) || v == 0 {
		return "?"
	}
	return FmtUsd(v) + star + "+?"
}

// EstimatedNote is the note for USD estimated by family, or "" when there is none. Bases are
// grouped in the order of their first model.
func EstimatedNote(models metrics.ModelBasis) string {
	var order []string
	by := map[string][]string{}
	for _, m := range models.Keys() {
		basis, _ := models.Get(m)
		if _, ok := by[basis]; !ok {
			order = append(order, basis)
		}
		by[basis] = append(by[basis], m)
	}
	if len(order) == 0 {
		return ""
	}
	parts := make([]string, len(order))
	for i, b := range order {
		parts[i] = "estimated with " + b + " rates: " + strings.Join(by[b], ", ")
	}
	return "(" + strings.Join(parts, "; ") + " — not in core/pricing.json)"
}

// FmtPct: v with digits decimals and a percent sign. NaN -> "?".
func FmtPct(v float64, digits int) string {
	if !finite(v) {
		return "?"
	}
	return jscompat.ToFixed(v, digits) + "%"
}

// FmtRatio is a small ratio such as calls per turn: 0.8 · 4.2 · 15 · 120. NaN -> "?".
func FmtRatio(v float64) string {
	if !finite(v) {
		return "?"
	}
	if v < 10 {
		return jscompat.ToFixed(v, 1)
	}
	return num(jscompat.Round(v))
}

// FmtDuration: 45s · 12m · 2h 05m · 3d 4h. NaN or negative -> "?".
func FmtDuration(ms float64) string {
	if !finite(ms) || ms < 0 {
		return "?"
	}
	s := jscompat.Round(ms / 1000)
	if s < 60 {
		return num(s) + "s"
	}
	m := math.Floor(s / 60)
	if m < 60 {
		return num(m) + "m"
	}
	h := math.Floor(m / 60)
	if h < 24 {
		return num(h) + "h " + jscompat.PadStart(num(math.Mod(m, 60)), 2, "0") + "m"
	}
	return num(math.Floor(h/24)) + "d " + num(math.Mod(h, 24)) + "h"
}

func pad2(n int) string { return jscompat.PadStart(strconv.Itoa(n), 2, "0") }

// FmtDate is `YYYY-MM-DD[ HH:mm]` in loc. A falsy ts (0 or NaN) gives "?"; a time outside the Date
// range prints as JS does for an invalid date.
func FmtDate(ts float64, loc *time.Location, withTime bool) string {
	if ts == 0 || math.IsNaN(ts) {
		return "?"
	}
	y, mon, d, h, min, _, ok := jscompat.LocalTime(ts, loc)
	if !ok {
		if withTime {
			return "NaN-NaN-NaN NaN:NaN"
		}
		return "NaN-NaN-NaN"
	}
	date := strconv.Itoa(y) + "-" + pad2(mon+1) + "-" + pad2(d)
	if withTime {
		return date + " " + pad2(h) + ":" + pad2(min)
	}
	return date
}

// Gauge is `▰▰▰▰▱▱▱▱` (cells wide); NaN -> all dim.
func Gauge(pct float64, cells int) string {
	if !finite(pct) {
		return strings.Repeat("▱", cells)
	}
	filled := int(math.Max(0, math.Min(float64(cells), jscompat.Round((pct/100)*float64(cells)))))
	return strings.Repeat("▰", filled) + strings.Repeat("▱", cells-filled)
}

var named = map[string]string{
	"bold": "1", "dim": "2",
	"black": "30", "red": "31", "green": "32", "yellow": "33", "blue": "34", "magenta": "35", "cyan": "36", "white": "37",
	"muted": "38;5;245", "gray": "38;5;245", "grey": "38;5;245",
}

var (
	hexRe   = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	indexRe = regexp.MustCompile(`^[0-9]{1,3}$`)
)

func ansiCodes(style string) []string {
	var codes []string
	seen := map[string]bool{}
	add := func(c string) {
		if !seen[c] {
			seen[c] = true
			codes = append(codes, c)
		}
	}
	for _, tok := range strings.Split(jscompat.CollapseSpace(jscompat.Trim(style)), " ") {
		if tok == "" || tok == "text" {
			continue
		}
		bg := strings.HasPrefix(tok, "bg:")
		if bg {
			tok = tok[3:]
		}
		if code, ok := named[tok]; ok {
			switch {
			case bg && len(code) == 2 && code[0] == '3':
				add("4" + code[1:])
			case bg && strings.HasPrefix(code, "38;"):
				add("48;" + code[3:])
			case !bg:
				add(code)
			}
		} else if hexRe.MatchString(tok) {
			base := "38"
			if bg {
				base = "48"
			}
			var rgb [3]int64
			for i := range rgb {
				rgb[i], _ = strconv.ParseInt(tok[1+2*i:3+2*i], 16, 32)
			}
			add(base + ";2;" + strconv.FormatInt(rgb[0], 10) + ";" + strconv.FormatInt(rgb[1], 10) + ";" + strconv.FormatInt(rgb[2], 10))
		} else if indexRe.MatchString(tok) {
			if n, _ := strconv.Atoi(tok); n <= 255 {
				base := "38"
				if bg {
					base = "48"
				}
				add(base + ";5;" + strconv.Itoa(n))
			}
		}
	}
	return codes
}

// Colorize wraps text in the ANSI codes of style (empty style or enabled=false: unchanged).
func Colorize(text, style string, enabled bool) string {
	if !enabled || style == "" {
		return text
	}
	codes := ansiCodes(style)
	if len(codes) == 0 {
		return text
	}
	return "\x1b[" + strings.Join(codes, ";") + "m" + text + "\x1b[0m"
}

// Severity is "" below warn, "warn" from warn and "crit" from crit. NaN never matches (also in
// the thresholds).
func Severity(v, warn, crit float64) string {
	if !finite(v) {
		return ""
	}
	if v >= crit {
		return "crit"
	}
	if v >= warn {
		return "warn"
	}
	return ""
}

// Col is a table column.
type Col struct {
	Label string
	Right bool
}

// Table is a plain-text table; widths count UTF-16 units like JS .length.
func Table(cols []Col, cells [][]string) string {
	widths := make([]int, len(cols))
	for i, c := range cols {
		widths[i] = jscompat.UTF16Len(c.Label)
		for _, row := range cells {
			if w := jscompat.UTF16Len(row[i]); w > widths[i] {
				widths[i] = w
			}
		}
	}
	line := func(vals []string) string {
		parts := make([]string, len(vals))
		for i, v := range vals {
			if cols[i].Right {
				parts[i] = jscompat.PadStart(v, widths[i], " ")
			} else {
				parts[i] = jscompat.PadEnd(v, widths[i], " ")
			}
		}
		return jscompat.TrimEnd(strings.Join(parts, "  "))
	}
	labels := make([]string, len(cols))
	dashes := make([]string, len(cols))
	for i, c := range cols {
		labels[i] = c.Label
		dashes[i] = strings.Repeat("-", widths[i])
	}
	out := []string{line(labels), line(dashes)}
	for _, row := range cells {
		out = append(out, line(row))
	}
	return strings.Join(out, "\n")
}

// Clip truncates to max UTF-16 units with an ellipsis.
func Clip(s string, max int) string {
	if jscompat.UTF16Len(s) > max {
		n := max - 1
		if n < 0 {
			n = 0
		}
		return jscompat.UTF16Prefix(s, n) + "…"
	}
	return s
}

var sinceRe = regexp.MustCompile(`^([0-9]+)([smhdw])$`)

var sinceMult = map[string]float64{"s": 1000, "m": 60_000, "h": 3_600_000, "d": 86_400_000, "w": 604_800_000}

// ParseSince reads `7d` · `24h` · `30m` or an ISO date into an epoch-ms lower bound; false when
// unparseable. Date-times without an offset are read in loc.
func ParseSince(s string, now int64, loc *time.Location) (int64, bool) {
	if s == "" {
		return 0, false
	}
	if m := sinceRe.FindStringSubmatch(jscompat.Trim(s)); m != nil {
		n, _ := strconv.ParseFloat(m[1], 64)
		return int64(float64(now) - float64(n*sinceMult[m[2]])), true
	}
	return jscompat.ParseDateIn(s, loc)
}

// Opt is an option of Args: a string, or the boolean true (IsFlag).
type Opt struct {
	Value  string
	IsFlag bool
}

// Args is the result of ParseArgs.
type Args struct {
	Opts       map[string]Opt
	Positional []string
}

// ParseArgs is the tiny argv parser: `--key value`, `--key=value`, `--flag`, positional. The last
// occurrence of a key wins.
func ParseArgs(argv []string) Args {
	out := Args{Opts: map[string]Opt{}}
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		if !strings.HasPrefix(a, "--") {
			out.Positional = append(out.Positional, a)
			continue
		}
		if eq := strings.Index(a, "="); eq != -1 {
			out.Opts[a[2:eq]] = Opt{Value: a[eq+1:]}
		} else if i+1 < len(argv) && !strings.HasPrefix(argv[i+1], "--") {
			i++
			out.Opts[a[2:]] = Opt{Value: argv[i]}
		} else {
			out.Opts[a[2:]] = Opt{IsFlag: true}
		}
	}
	return out
}

// Str is the string value of an option (false when absent or a bare flag).
func (a Args) Str(k string) (string, bool) {
	o, ok := a.Opts[k]
	if !ok || o.IsFlag {
		return "", false
	}
	return o.Value, true
}

// Flag is the JS truthiness of an option: a bare flag or a non-empty string.
func (a Args) Flag(k string) bool {
	o, ok := a.Opts[k]
	return ok && (o.IsFlag || o.Value != "")
}
