package jscompat

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
)

// node is a parsed JSON value that keeps what JSON.stringify(JSON.parse(text)) would write.
type node struct {
	kind byte // 'o' object, 'a' array, 's' string, 'n' number, 'l' literal (true/false/null)
	text string
	kids []*node  // array items, or object values in key order
	keys []string // object keys, same order as kids
}

// Restringify is JSON.stringify(JSON.parse(raw), null, indent): it re-emits JSON text keeping the
// key order and spelling every value the way JS does (numbers through String, so "1e-7" where Go
// writes "1e-07"; a number beyond the double range becomes null; "<", ">", "&" and U+2028 are not
// escaped). An empty indent gives the compact form. Callers pass json.Marshal output (ordered
// maps and structs) or raw stdin. As JS objects, a repeated key keeps the first position with the
// last value, and keys that are array indexes ("0", "12") come first in ascending order. Lone
// surrogates cannot be represented in a Go string and are not reproduced.
func Restringify(raw []byte, indent string) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	n, err := parseNode(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("jscompat: trailing data after JSON value")
	}
	var b strings.Builder
	emit(&b, n, indent, "")
	return []byte(b.String()), nil
}

func parseNode(dec *json.Decoder) (*node, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		if t == '[' {
			n := &node{kind: 'a'}
			for dec.More() {
				k, err := parseNode(dec)
				if err != nil {
					return nil, err
				}
				n.kids = append(n.kids, k)
			}
			if _, err := dec.Token(); err != nil {
				return nil, err
			}
			return n, nil
		}
		n := &node{kind: 'o'}
		index := map[string]int{}
		for dec.More() {
			kt, err := dec.Token()
			if err != nil {
				return nil, err
			}
			key, ok := kt.(string)
			if !ok {
				return nil, fmt.Errorf("jscompat: bad object key %v", kt)
			}
			v, err := parseNode(dec)
			if err != nil {
				return nil, err
			}
			if i, dup := index[key]; dup {
				n.kids[i] = v
				continue
			}
			index[key] = len(n.keys)
			n.keys = append(n.keys, key)
			n.kids = append(n.kids, v)
		}
		if _, err := dec.Token(); err != nil {
			return nil, err
		}
		orderKeys(n)
		return n, nil
	case string:
		return &node{kind: 's', text: t}, nil
	case json.Number:
		f, err := strconv.ParseFloat(string(t), 64)
		if err != nil && !math.IsInf(f, 0) {
			return nil, err
		}
		if math.IsInf(f, 0) {
			return &node{kind: 'l', text: "null"}, nil
		}
		return &node{kind: 'n', text: formatNumber(f)}, nil
	case bool:
		return &node{kind: 'l', text: strconv.FormatBool(t)}, nil
	}
	return &node{kind: 'l', text: "null"}, nil
}

// arrayIndex reports whether k is a canonical array index (JS orders those first in an object).
func arrayIndex(k string) (uint64, bool) {
	if k == "" || len(k) > 10 || (len(k) > 1 && k[0] == '0') {
		return 0, false
	}
	n, err := strconv.ParseUint(k, 10, 64)
	if err != nil || n >= 1<<32-1 {
		return 0, false
	}
	return n, true
}

func orderKeys(n *node) {
	idx := make([]int, len(n.keys))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool {
		na, oka := arrayIndex(n.keys[idx[a]])
		nb, okb := arrayIndex(n.keys[idx[b]])
		switch {
		case oka && okb:
			return na < nb
		case oka != okb:
			return oka
		}
		return false
	})
	keys := make([]string, len(idx))
	kids := make([]*node, len(idx))
	for i, j := range idx {
		keys[i], kids[i] = n.keys[j], n.kids[j]
	}
	n.keys, n.kids = keys, kids
}

func emit(b *strings.Builder, n *node, indent, cur string) {
	switch n.kind {
	case 's':
		quote(b, n.text)
	case 'n', 'l':
		b.WriteString(n.text)
	case 'a', 'o':
		open, closer := "[", "]"
		if n.kind == 'o' {
			open, closer = "{", "}"
		}
		if len(n.kids) == 0 {
			b.WriteString(open + closer)
			return
		}
		inner := cur + indent
		b.WriteString(open)
		for i, k := range n.kids {
			if i > 0 {
				b.WriteByte(',')
			}
			if indent != "" {
				b.WriteString("\n" + inner)
			}
			if n.kind == 'o' {
				quote(b, n.keys[i])
				b.WriteByte(':')
				if indent != "" {
					b.WriteByte(' ')
				}
			}
			emit(b, k, indent, inner)
		}
		if indent != "" {
			b.WriteString("\n" + cur)
		}
		b.WriteString(closer)
	}
}

const hexDigits = "0123456789abcdef"

// quote writes s as JSON.stringify does: only the quote, the backslash and C0 controls are escaped.
func quote(b *strings.Builder, s string) {
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				b.WriteString(`\u00`)
				b.WriteByte(hexDigits[r>>4])
				b.WriteByte(hexDigits[r&0xF])
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
}
