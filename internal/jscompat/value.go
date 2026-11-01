package jscompat

import (
	"math"
	"strconv"
	"strings"
)

type undefinedT struct{}

// MarshalJSON makes Undefined disappear the way JSON.stringify turns it into null in arrays.
func (undefinedT) MarshalJSON() ([]byte, error) { return []byte("null"), nil }

// Undefined is JS undefined. json.Unmarshal into any never produces it (null is nil).
var Undefined any = undefinedT{}

// IsUndefined reports whether v is Undefined.
func IsUndefined(v any) bool {
	_, ok := v.(undefinedT)
	return ok
}

// Get walks keys over objects (v?.a?.b): a missing key or a non-object gives Undefined.
func Get(v any, keys ...string) any {
	for _, k := range keys {
		m, ok := v.(map[string]any)
		if !ok {
			return Undefined
		}
		next, ok := m[k]
		if !ok {
			return Undefined
		}
		v = next
	}
	return v
}

// Truthy is JS truthiness over a decoded JSON value.
func Truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case undefinedT:
		return false
	case bool:
		return x
	case float64:
		return x != 0 && !math.IsNaN(x)
	case string:
		return x != ""
	}
	return true
}

// Or is a || b || c: the first truthy value, else the last (Undefined when there are none).
func Or(vs ...any) any {
	if len(vs) == 0 {
		return Undefined
	}
	for _, v := range vs {
		if Truthy(v) {
			return v
		}
	}
	return vs[len(vs)-1]
}

// Num is typeof v === 'number' && Number.isFinite(v).
func Num(v any) (float64, bool) {
	f, ok := v.(float64)
	if !ok || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	return f, true
}

// String is JS String(v) for decoded JSON values.
func String(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case undefinedT:
		return "undefined"
	case bool:
		return strconv.FormatBool(x)
	case float64:
		return formatNumber(x)
	case string:
		return x
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			if e == nil || IsUndefined(e) {
				continue
			}
			parts[i] = String(e)
		}
		return strings.Join(parts, ",")
	}
	return "[object Object]"
}

// formatNumber is Number.prototype.toString (ECMAScript Number::toString).
func formatNumber(x float64) string {
	switch {
	case math.IsNaN(x):
		return "NaN"
	case x == 0:
		return "0"
	case math.IsInf(x, 1):
		return "Infinity"
	case math.IsInf(x, -1):
		return "-Infinity"
	}
	sign := ""
	if x < 0 {
		sign, x = "-", -x
	}
	e := strconv.FormatFloat(x, 'e', -1, 64) // d.ddde±XX, shortest digits
	mant, expStr, _ := strings.Cut(e, "e")
	exp, _ := strconv.Atoi(expStr)
	digits := strings.Replace(mant, ".", "", 1)
	k, n := len(digits), exp+1
	switch {
	case k <= n && n <= 21:
		return sign + digits + strings.Repeat("0", n-k)
	case 0 < n && n <= 21:
		return sign + digits[:n] + "." + digits[n:]
	case -6 < n && n <= 0:
		return sign + "0." + strings.Repeat("0", -n) + digits
	}
	es := "+"
	if n-1 < 0 {
		es = "-"
	}
	ae := n - 1
	if ae < 0 {
		ae = -ae
	}
	if k == 1 {
		return sign + digits + "e" + es + strconv.Itoa(ae)
	}
	return sign + digits[:1] + "." + digits[1:] + "e" + es + strconv.Itoa(ae)
}
