package jscompat

import (
	"math"
	"time"
)

const maxTime = 8_640_000_000_000_000 // ms, ECMAScript TimeClip limit

// ParseDate is Date.parse for the ECMAScript date-time string format:
// YYYY[-MM[-DD]][THH:mm[:ss[.fff]]][Z|±HH:mm] and ±YYYYYY. It returns epoch milliseconds, or
// false where JS gives NaN. Date-only forms are UTC; a date-time without offset is local time
// (time.Local), as V8. Like V8 the day may be 1..31 in any month (Feb 30 rolls into March); the
// fraction is truncated to milliseconds. Non-strings and anything outside the format give false.
func ParseDate(v any) (int64, bool) { return ParseDateIn(v, time.Local) }

// ParseDateIn is ParseDate with the zone that reads date-times without an offset.
func ParseDateIn(v any, loc *time.Location) (int64, bool) {
	s, ok := v.(string)
	if !ok {
		return 0, false
	}
	p := &dateParser{s: s, loc: loc}
	return p.parse()
}

// LocalTime is the Date getters (getFullYear, getMonth, getDate, getHours, getMinutes, getDay)
// of the instant ms in loc. mon is 0-based like getMonth and weekday 0 is Sunday. ms goes through
// TimeClip: the fraction is truncated toward zero and NaN or |ms| > 8.64e15 gives ok=false (JS
// getters give NaN, which prints as "NaN").
func LocalTime(ms float64, loc *time.Location) (y, mon, d, h, min, weekday int, ok bool) {
	if math.IsNaN(ms) || math.Abs(ms) > maxTime {
		return 0, 0, 0, 0, 0, 0, false
	}
	t := time.UnixMilli(int64(math.Trunc(ms))).In(loc)
	return t.Year(), int(t.Month()) - 1, t.Day(), t.Hour(), t.Minute(), int(t.Weekday()), true
}

type dateParser struct {
	s   string
	i   int
	loc *time.Location
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
		out := localToUnix(year, month, day, hour, minute, sec, p.loc)*1000 + int64(ms)
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

// localToUnix is the epoch second of a wall-clock time in loc the way V8 resolves it: a time that
// happens twice (clocks going back) is the first one, and a time that does not exist (clocks going
// forward) is read with the offset in effect before the change. time.Date picks the second
// instance in the first case, so the candidates are checked here.
func localToUnix(year, month, day, hour, minute, sec int, loc *time.Location) int64 {
	wall := time.Date(year, time.Month(month), day, hour, minute, sec, 0, time.UTC).Unix()
	offsetAt := func(u int64) int {
		_, o := time.Unix(u, 0).In(loc).Zone()
		return o
	}
	before, after := offsetAt(wall-86400), offsetAt(wall+86400)
	best, found := int64(0), false
	for _, off := range [2]int{before, after} {
		u := wall - int64(off)
		if offsetAt(u) == off && (!found || u < best) {
			best, found = u, true
		}
	}
	if !found {
		return wall - int64(before)
	}
	return best
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
