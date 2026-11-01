package jscompat

import "time"

const maxTime = 8_640_000_000_000_000 // ms, ECMAScript TimeClip limit

// ParseDate is Date.parse for the ECMAScript date-time string format:
// YYYY[-MM[-DD]][THH:mm[:ss[.fff]]][Z|±HH:mm] and ±YYYYYY. It returns epoch milliseconds, or
// false where JS gives NaN. Date-only forms are UTC; a date-time without offset is local time
// (time.Local), as V8. Like V8 the day may be 1..31 in any month (Feb 30 rolls into March); the
// fraction is truncated to milliseconds. Non-strings and anything outside the format give false.
func ParseDate(v any) (int64, bool) {
	s, ok := v.(string)
	if !ok {
		return 0, false
	}
	p := &dateParser{s: s}
	return p.parse()
}

type dateParser struct {
	s string
	i int
}

func (p *dateParser) digits(n int) (int, bool) {
	if p.i+n > len(p.s) {
		return 0, false
	}
	v := 0
	for j := 0; j < n; j++ {
		c := p.s[p.i+j]
		if c < '0' || c > '9' {
			return 0, false
		}
		v = v*10 + int(c-'0')
	}
	p.i += n
	return v, true
}

func (p *dateParser) eat(c byte) bool {
	if p.i < len(p.s) && p.s[p.i] == c {
		p.i++
		return true
	}
	return false
}

func (p *dateParser) parse() (int64, bool) {
	var year int
	var ok bool
	if p.i < len(p.s) && (p.s[p.i] == '+' || p.s[p.i] == '-') {
		neg := p.s[p.i] == '-'
		p.i++
		if year, ok = p.digits(6); !ok {
			return 0, false
		}
		if neg {
			if year == 0 {
				return 0, false // -000000 is invalid
			}
			year = -year
		}
	} else if year, ok = p.digits(4); !ok {
		return 0, false
	}
	month, day := 1, 1
	if p.eat('-') {
		if month, ok = p.digits(2); !ok || month < 1 || month > 12 {
			return 0, false
		}
		if p.eat('-') {
			if day, ok = p.digits(2); !ok || day < 1 || day > 31 {
				return 0, false
			}
		}
	}
	hour, minute, sec, ms := 0, 0, 0, 0
	hasTime := false
	if p.eat('T') {
		hasTime = true
		if hour, ok = p.digits(2); !ok || hour > 24 || !p.eat(':') {
			return 0, false
		}
		if minute, ok = p.digits(2); !ok || minute > 59 {
			return 0, false
		}
		if p.eat(':') {
			if sec, ok = p.digits(2); !ok || sec > 59 {
				return 0, false
			}
			if p.eat('.') {
				n := 0
				for p.i < len(p.s) && p.s[p.i] >= '0' && p.s[p.i] <= '9' {
					if n < 3 {
						ms = ms*10 + int(p.s[p.i]-'0')
					}
					n++
					p.i++
				}
				if n == 0 {
					return 0, false
				}
				for ; n < 3; n++ {
					ms *= 10
				}
			}
		}
		if hour == 24 && (minute != 0 || sec != 0 || ms != 0) {
			return 0, false
		}
	}
	offsetMin := 0
	hasOffset := false
	if hasTime {
		switch {
		case p.eat('Z'):
			hasOffset = true
		case p.i < len(p.s) && (p.s[p.i] == '+' || p.s[p.i] == '-'):
			sign := 1
			if p.s[p.i] == '-' {
				sign = -1
			}
			p.i++
			oh, ok1 := p.digits(2)
			if !ok1 || oh > 23 || !p.eat(':') {
				return 0, false
			}
			om, ok2 := p.digits(2)
			if !ok2 || om > 59 {
				return 0, false
			}
			offsetMin = sign * (oh*60 + om)
			hasOffset = true
		}
	}
	if p.i != len(p.s) {
		return 0, false
	}
	if hasTime && !hasOffset {
		// Local time, as V8. Only years time.Date handles sanely get here.
		t := time.Date(year, time.Month(month), day, hour, minute, sec, 0, time.Local)
		out := t.Unix()*1000 + int64(ms)
		if out > maxTime || out < -maxTime {
			return 0, false
		}
		return out, true
	}
	days := daysFromCivil(int64(year), int64(month), 1) + int64(day-1)
	out := days*86_400_000 + int64(hour)*3_600_000 + int64(minute)*60_000 + int64(sec)*1000 + int64(ms) - int64(offsetMin)*60_000
	if out > maxTime || out < -maxTime {
		return 0, false
	}
	return out, true
}

// daysFromCivil is the proleptic Gregorian day number since 1970-01-01.
func daysFromCivil(y, m, d int64) int64 {
	if m <= 2 {
		y--
	}
	era := y / 400
	if y < 0 {
		era = (y - 399) / 400
	}
	yoe := y - era*400
	mp := (m + 9) % 12
	doy := (153*mp+2)/5 + d - 1
	doe := yoe*365 + yoe/4 - yoe/100 + doy
	return era*146097 + doe - 719468
}
