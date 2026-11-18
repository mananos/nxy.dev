package platform

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mananos/nxy.dev/v2/internal/jscompat"
)

// ErrNotObject: the file holds JSON that is not an object (nxy never overwrites it).
var ErrNotObject = errors.New("no es un objeto JSON")

// JSONDoc is a JSON object that keeps its key order, so a file nxy edits (Claude Code's
// settings.json, .nxy/config.json) only changes where nxy wrote, as JSON.stringify of the 1.x did.
type JSONDoc struct {
	keys []string
	vals map[string]any // *JSONDoc, json.RawMessage (any other value)
}

// NewJSONDoc is an empty object.
func NewJSONDoc() *JSONDoc { return &JSONDoc{vals: map[string]any{}} }

// ParseJSONDoc parses an object; anything else is ErrNotObject, broken JSON an error.
func ParseJSONDoc(data []byte) (*JSONDoc, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, ErrNotObject
	}
	doc, err := parseObject(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("datos de más después del objeto JSON")
	}
	return doc, nil
}

func parseObject(dec *json.Decoder) (*JSONDoc, error) {
	doc := NewJSONDoc()
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := tok.(string)
		if !ok {
			return nil, errors.New("clave JSON inválida")
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, err
		}
		var v any = raw
		if t := bytes.TrimSpace(raw); len(t) > 0 && t[0] == '{' {
			inner := json.NewDecoder(bytes.NewReader(t))
			inner.UseNumber()
			if _, err := inner.Token(); err != nil {
				return nil, err
			}
			child, err := parseObject(inner)
			if err != nil {
				return nil, err
			}
			v = child
		}
		doc.put(key, v)
	}
	if _, err := dec.Token(); err != nil { // closing }
		return nil, err
	}
	return doc, nil
}

func (d *JSONDoc) put(key string, v any) {
	if _, ok := d.vals[key]; !ok {
		d.keys = append(d.keys, key)
	}
	d.vals[key] = v
}

func (d *JSONDoc) del(key string) {
	if _, ok := d.vals[key]; !ok {
		return
	}
	delete(d.vals, key)
	for i, k := range d.keys {
		if k == key {
			d.keys = append(d.keys[:i], d.keys[i+1:]...)
			break
		}
	}
}

// Has reports whether path exists (every step but the last must be an object).
func (d *JSONDoc) Has(path ...string) bool {
	_, ok := d.Get(path...)
	return ok
}

// Get returns the decoded value at path (objects as map[string]any).
func (d *JSONDoc) Get(path ...string) (any, bool) {
	cur := d
	for i, k := range path {
		v, ok := cur.vals[k]
		if !ok {
			return nil, false
		}
		if i == len(path)-1 {
			return decodeValue(v), true
		}
		child, ok := v.(*JSONDoc)
		if !ok {
			return nil, false
		}
		cur = child
	}
	return decodeValue(cur), true
}

func decodeValue(v any) any {
	switch x := v.(type) {
	case *JSONDoc:
		m := make(map[string]any, len(x.keys))
		for _, k := range x.keys {
			m[k] = decodeValue(x.vals[k])
		}
		return m
	case json.RawMessage:
		var out any
		_ = json.Unmarshal(x, &out)
		return out
	}
	return nil
}

// Set writes value at path, creating (or replacing non-object) parents; a new key goes last.
func (d *JSONDoc) Set(path []string, value any) error {
	if len(path) == 0 {
		return errors.New("ruta vacía")
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	cur := d
	for _, k := range path[:len(path)-1] {
		child, ok := cur.vals[k].(*JSONDoc)
		if !ok {
			child = NewJSONDoc()
			cur.put(k, child)
		}
		cur = child
	}
	var v any = json.RawMessage(raw)
	if len(raw) > 0 && raw[0] == '{' {
		parsed, err := ParseJSONDoc(raw)
		if err != nil {
			return err
		}
		v = parsed
	}
	cur.put(path[len(path)-1], v)
	return nil
}

// Delete removes path and prunes the objects it leaves empty (setPath of core/configedit.mjs with null).
func (d *JSONDoc) Delete(path ...string) {
	if len(path) == 0 {
		return
	}
	if len(path) == 1 {
		d.del(path[0])
		return
	}
	child, ok := d.vals[path[0]].(*JSONDoc)
	if !ok {
		return
	}
	child.Delete(path[1:]...)
	if len(child.keys) == 0 {
		d.del(path[0])
	}
}

// MarshalJSON writes the keys in order.
func (d *JSONDoc) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, k := range d.keys {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		b.Write(kb)
		b.WriteByte(':')
		vb, err := json.Marshal(d.vals[k])
		if err != nil {
			return nil, err
		}
		b.Write(vb)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// Text is JSON.stringify(doc, null, 2) + "\n", the spelling the 1.x wrote these files with.
func (d *JSONDoc) Text() ([]byte, error) {
	raw, err := json.Marshal(d)
	if err != nil {
		return nil, err
	}
	out, err := jscompat.Restringify(raw, "  ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// ReadJSONDoc reads an object file: a missing file is an empty doc with exists=false; a broken
// one is an error (callers never overwrite it).
func ReadJSONDoc(path string) (doc *JSONDoc, exists bool, err error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return NewJSONDoc(), false, nil
	}
	if err != nil {
		return nil, true, err
	}
	doc, err = ParseJSONDoc(data)
	if err != nil {
		return nil, true, err
	}
	return doc, true, nil
}

// WriteJSONDoc writes doc atomically, creating the directory; it returns the bytes written.
func WriteJSONDoc(path string, doc *JSONDoc) (int, error) {
	text, err := doc.Text()
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return 0, err
	}
	return len(text), WriteAtomic(path, text)
}

// BackupKeep is how many `<file>.bak-nxy-<ms>` copies nxy keeps per file.
const BackupKeep = 3

// BackupAndPrune copies path to `<path>.bak-nxy-<ms>` and removes nxy's older backups beyond
// BackupKeep (never other tools' `.bak-<ts>`), like backupAndPrune of the 1.x config entry.
func BackupAndPrune(path string, now time.Time) (backup string, kept int, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", 0, err
	}
	ts := now.UnixMilli()
	for {
		backup = fmt.Sprintf("%s.bak-nxy-%d", path, ts)
		if _, err := os.Stat(backup); errors.Is(err, os.ErrNotExist) {
			break
		}
		ts++
	}
	if err := os.WriteFile(backup, data, 0o644); err != nil {
		return "", 0, err
	}
	prefix := filepath.Base(path) + ".bak-nxy-"
	entries, _ := os.ReadDir(filepath.Dir(path))
	type bk struct {
		name string
		ts   int64
	}
	var mine []bk
	for _, e := range entries {
		n := e.Name()
		if !strings.HasPrefix(n, prefix) {
			continue
		}
		t, err := strconv.ParseInt(n[len(prefix):], 10, 64)
		if err != nil || t < 0 {
			continue
		}
		mine = append(mine, bk{n, t})
	}
	sort.Slice(mine, func(i, j int) bool { return mine[i].ts > mine[j].ts })
	for _, old := range mine[min(len(mine), BackupKeep):] {
		_ = os.Remove(filepath.Join(filepath.Dir(path), old.name))
	}
	return backup, min(len(mine), BackupKeep), nil
}
