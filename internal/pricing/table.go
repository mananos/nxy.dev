package pricing

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// FastRates is the fast-mode input/output price (USD per 1M tokens).
type FastRates struct {
	Input  float64 `json:"input"`
	Output float64 `json:"output"`
}

// AboveRates is the tariff of a request whose prompt is over Threshold tokens.
type AboveRates struct {
	Threshold    float64 `json:"threshold"`
	Input        float64 `json:"input"`
	CacheWrite5m float64 `json:"cache_write_5m"`
	CacheWrite1h float64 `json:"cache_write_1h"`
	CacheRead    float64 `json:"cache_read"`
	Output       float64 `json:"output"`
}

// ModelPrice is one row of pricing.json (USD per 1M tokens).
type ModelPrice struct {
	Input        float64     `json:"input"`
	CacheWrite5m float64     `json:"cache_write_5m"`
	CacheWrite1h float64     `json:"cache_write_1h"`
	CacheRead    float64     `json:"cache_read"`
	Output       float64     `json:"output"`
	Fast         *FastRates  `json:"fast,omitempty"`
	Above        *AboveRates `json:"above,omitempty"`
}

// Table is a set of prices that keeps the key order of the file (the 1.x relies on object
// insertion order for ties between equal versions).
type Table struct {
	Keys   []string
	Models map[string]ModelPrice

	idx []keyInfo // family and version of Keys[i], computed once by the parsers (read-only)
}

type keyInfo struct {
	fam string
	ok  bool
	ver []float64
}

// Match is the result of a price lookup.
type Match struct {
	Price     ModelPrice
	Key       string
	Estimated bool
}

//go:embed pricing.json
var embedded []byte

var (
	defaultOnce  sync.Once
	defaultTable Table
)

// Default is the table embedded in the binary (pricing.json).
func Default() Table {
	defaultOnce.Do(func() {
		t, err := ParseFile(embedded)
		if err != nil {
			panic("pricing: embedded pricing.json: " + err.Error())
		}
		defaultTable = t
	})
	return defaultTable
}

// ParseFile reads a pricing.json document ({"models": {...}, ...}).
func ParseFile(data []byte) (Table, error) {
	dec := json.NewDecoder(strings.NewReader(string(data)))
	if err := expectDelim(dec, '{'); err != nil {
		return Table{}, err
	}
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			return Table{}, err
		}
		if k == "models" {
			return decodeModels(dec)
		}
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return Table{}, err
		}
	}
	return Table{}, fmt.Errorf("no models key")
}

// ParseModels reads a models object ({"claude-...": {...}}) keeping its key order.
func ParseModels(data []byte) (Table, error) {
	return decodeModels(json.NewDecoder(strings.NewReader(string(data))))
}

func expectDelim(dec *json.Decoder, d json.Delim) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if got, ok := tok.(json.Delim); !ok || got != d {
		return fmt.Errorf("expected %q, got %v", d, tok)
	}
	return nil
}

func decodeModels(dec *json.Decoder) (Table, error) {
	t := Table{Models: map[string]ModelPrice{}}
	if err := expectDelim(dec, '{'); err != nil {
		return t, err
	}
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			return t, err
		}
		key, ok := k.(string)
		if !ok {
			return t, fmt.Errorf("bad key %v", k)
		}
		var p ModelPrice
		if err := dec.Decode(&p); err != nil {
			return t, err
		}
		if _, dup := t.Models[key]; !dup {
			t.Keys = append(t.Keys, key)
		}
		t.Models[key] = p
	}
	if err := expectDelim(dec, '}'); err != nil {
		return t, err
	}
	t.idx = make([]keyInfo, len(t.Keys))
	for i, k := range t.Keys {
		f, ok := FamilyOf(k)
		t.idx[i] = keyInfo{fam: f, ok: ok, ver: VersionOf(k)}
	}
	return t, nil
}

// TierFor is the rate row that applies to ONE request by prompt length: the price as is when it
// has no `above` or the prompt is <= threshold; otherwise the base row with the `above` rates.
func TierFor(price ModelPrice, promptTokens float64) ModelPrice {
	a := price.Above
	if a == nil || !(promptTokens > a.Threshold) {
		return price
	}
	out := price
	out.Above = nil
	out.Input, out.CacheWrite5m, out.CacheWrite1h, out.CacheRead, out.Output = a.Input, a.CacheWrite5m, a.CacheWrite1h, a.CacheRead, a.Output
	return out
}

var normSteps = []struct {
	re   *regexp.Regexp
	repl string
}{
	{regexp.MustCompile(`^(?:us|eu|apac|global)\.`), ""},
	{regexp.MustCompile(`^anthropic\.`), ""},
	{regexp.MustCompile(`-v\d+:\d+$`), ""},
	{regexp.MustCompile(`@\d{8}$`), ""},
	{regexp.MustCompile(`\[[^\]]*\]$`), ""},
	{regexp.MustCompile(`-\d{8}$`), ""},
	{regexp.MustCompile(`-latest$`), ""},
}

// NormalizeModel reduces a model id (API, Bedrock, Vertex) to its price table key.
func NormalizeModel(id string) string {
	s := strings.ToLower(id)
	for _, st := range normSteps {
		s = st.re.ReplaceAllString(s, st.repl)
	}
	return s
}

var familyRe = regexp.MustCompile(`(opus|sonnet|haiku|fable|mythos)`)

// FamilyOf is opus|sonnet|haiku|fable|mythos found in the normalised id, or "" and false.
func FamilyOf(id string) (string, bool) {
	m := familyRe.FindString(NormalizeModel(id))
	return m, m != ""
}

var digitsRe = regexp.MustCompile(`\d+`)

// VersionOf lists the digit groups of a key: claude-sonnet-5-5 -> [5 5].
func VersionOf(key string) []float64 {
	ms := digitsRe.FindAllString(key, -1)
	out := make([]float64, 0, len(ms))
	for _, m := range ms {
		f, _ := strconv.ParseFloat(m, 64)
		out = append(out, f)
	}
	return out
}

func cmpVersion(a, b []float64) float64 {
	n := len(a)
	if len(b) > n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		var x, y float64
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if d := x - y; d != 0 {
			return d
		}
	}
	return 0
}

// PriceIn finds the price of a model: the exact key, or (estimated) the highest known version of
// the same family that is <= the requested one (the lowest above it when none is).
func (t Table) PriceIn(model string) (Match, bool) {
	id := NormalizeModel(model)
	if p, ok := t.Models[id]; ok {
		return Match{Price: p, Key: id}, true
	}
	fam, ok := FamilyOf(id)
	if !ok {
		return Match{}, false
	}
	want := VersionOf(id)
	type entry struct {
		k string
		v []float64
	}
	var same []entry
	for i, k := range t.Keys {
		if t.idx != nil && len(t.idx) == len(t.Keys) {
			if e := t.idx[i]; e.ok && e.fam == fam {
				same = append(same, entry{k, e.ver})
			}
			continue
		}
		if f, ok := FamilyOf(k); ok && f == fam {
			same = append(same, entry{k, VersionOf(k)})
		}
	}
	if len(same) == 0 {
		return Match{}, false
	}
	sort.SliceStable(same, func(i, j int) bool { return cmpVersion(same[i].v, same[j].v) < 0 })
	pick := same[0]
	for _, e := range same {
		if cmpVersion(e.v, want) <= 0 {
			pick = e
		}
	}
	return Match{Price: t.Models[pick.k], Key: pick.k, Estimated: true}, true
}
