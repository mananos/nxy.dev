package jscompat

import (
	"math"
	"math/big"
	"strconv"
	"strings"
)

// ToFixed is Number.prototype.toFixed(digits) for 0 <= digits <= 100. The double is taken as the
// exact rational it is, scaled by 10^digits and rounded with ties going up in magnitude (so 2.5
// gives "3" and 1.005 gives "1.00"). A negative x keeps its sign even when the result is zero
// (-0.001.toFixed(2) is "-0.00"); -0 itself is not negative ("0.00"). |x| >= 1e21 is String(x).
func ToFixed(x float64, digits int) string {
	if math.IsNaN(x) {
		return "NaN"
	}
	if math.Abs(x) >= 1e21 {
		return formatNumber(x)
	}
	sign := ""
	if x < 0 {
		sign, x = "-", -x
	}
	r := new(big.Rat).SetFloat64(x)
	r.Mul(r, new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(digits)), nil)))
	// floor(r + 1/2) = floor((2*num + den) / (2*den))
	num := new(big.Int).Lsh(r.Num(), 1)
	num.Add(num, r.Denom())
	den := new(big.Int).Lsh(r.Denom(), 1)
	n := num.Div(num, den).String()
	if digits > 0 {
		if len(n) <= digits {
			n = strings.Repeat("0", digits+1-len(n)) + n
		}
		n = n[:len(n)-digits] + "." + n[len(n)-digits:]
	}
	return sign + n
}

// Round is Math.round: ties go toward +Infinity (-0.5 gives -0, 2.5 gives 3). It is floor plus a
// >= 0.5 check, not floor(x+0.5), which is wrong for 0.49999999999999994.
func Round(x float64) float64 {
	if math.IsNaN(x) || math.IsInf(x, 0) || math.Abs(x) >= 1<<52 {
		return x
	}
	f := math.Floor(x)
	if x-f >= 0.5 {
		f++
	}
	if f == 0 && (x < 0 || math.Signbit(x)) {
		return math.Copysign(0, -1)
	}
	return f
}

// IsSpace is the JS \s set (WhiteSpace + LineTerminator) for one rune.
func IsSpace(r rune) bool { return isSpace(r) }

// isSpace is the JS \s / WhiteSpace + LineTerminator set.
func isSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xA0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return true
	}
	return r >= 0x2000 && r <= 0x200A
}

// ToNumber is the ECMAScript ToNumber of a decoded JSON value: null 0, booleans 0/1, numbers as
// they are, strings read as a numeric literal after trimming JS whitespace ("" and blank give 0;
// decimal, Infinity and 0x/0o/0b forms; anything else NaN). Arrays and objects give NaN here
// (JS would convert them through String); Undefined gives NaN.
func ToNumber(v any) float64 {
	switch x := v.(type) {
	case nil:
		return 0
	case bool:
		if x {
			return 1
		}
		return 0
	case float64:
		return x
	case string:
		return stringToNumber(x)
	}
	return math.NaN()
}

func stringToNumber(s string) float64 {
	s = Trim(s)
	if s == "" {
		return 0
	}
	if len(s) > 1 && s[0] == '0' {
		base := 0
		switch s[1] {
		case 'x', 'X':
			base = 16
		case 'o', 'O':
			base = 8
		case 'b', 'B':
			base = 2
		}
		if base != 0 {
			if strings.ContainsAny(s[2:], "_+- ") {
				return math.NaN()
			}
			n, ok := new(big.Int).SetString(s[2:], base)
			if !ok {
				return math.NaN()
			}
			f, _ := new(big.Float).SetInt(n).Float64()
			return f
		}
	}
	body := s
	sign := 1.0
	if body[0] == '+' || body[0] == '-' {
		if body[0] == '-' {
			sign = -1
		}
		body = body[1:]
	}
	if body == "Infinity" {
		return sign * math.Inf(1)
	}
	if !decimalLiteral(body) {
		return math.NaN()
	}
	f, err := strconv.ParseFloat(body, 64) // range errors give +-Inf, as JS
	if err != nil && !math.IsInf(f, 0) {
		return math.NaN()
	}
	return sign * f
}

// decimalLiteral matches (digits[.digits*] | .digits)([eE][+-]?digits)?
func decimalLiteral(s string) bool {
	i, n := 0, len(s)
	digits := func() int {
		st := i
		for i < n && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		return i - st
	}
	whole := digits()
	frac := 0
	if i < n && s[i] == '.' {
		i++
		frac = digits()
	}
	if whole == 0 && frac == 0 {
		return false
	}
	if i < n && (s[i] == 'e' || s[i] == 'E') {
		i++
		if i < n && (s[i] == '+' || s[i] == '-') {
			i++
		}
		if digits() == 0 {
			return false
		}
	}
	return i == n
}
