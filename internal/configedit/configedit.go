// Package configedit is the schema of the keys `nxy config` can change, and the pure helpers to
// validate a value, write it into a JSON object and tell where it comes from. It ports
// core/configedit.mjs for the keys the v2 reads: modules.filter, filter.engine and the model and
// effort of each role. The 1.x keys the v2 does not read (gate, flow, ui, tester, librarian) are
// not in the schema, and filter.engine and the v2 roles reviewer-quick and refuter are new.
package configedit

import "fmt"

// Models and Efforts are the values a role can take.
var (
	Models  = []string{"haiku", "sonnet", "opus", "fable"}
	Efforts = []string{"low", "medium", "high"}
	// Roles are the subagents whose model and effort the user can choose.
	Roles = []string{"planner", "implementer", "reviewer", "reviewer-quick", "refuter", "scout", "documenter"}
)

// Field is one editable key. Options are the valid values (strings or bools); null is always valid
// and means "back to the default" (the key is removed).
type Field struct {
	ID      string
	Path    []string
	Options []any
}

var fields []Field

func init() {
	for _, r := range Roles {
		fields = append(fields,
			Field{ID: "roles." + r + ".model", Path: []string{"roles", r, "model"}, Options: toAny(Models)},
			Field{ID: "roles." + r + ".effort", Path: []string{"roles", r, "effort"}, Options: toAny(Efforts)})
	}
	fields = append(fields,
		Field{ID: "modules.filter", Path: []string{"modules", "filter"}, Options: []any{true, false}},
		Field{ID: "filter.engine", Path: []string{"filter", "engine"}, Options: []any{"auto", "rtk", "off"}})
}

func toAny(s []string) []any {
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}

// Fields lists the editable keys in display order: the filter, then the roles.
func Fields() []Field {
	out := []Field{}
	for _, f := range fields {
		if f.ID == "modules.filter" || f.ID == "filter.engine" {
			out = append(out, f)
		}
	}
	for _, f := range fields {
		if f.ID != "modules.filter" && f.ID != "filter.engine" {
			out = append(out, f)
		}
	}
	return out
}

// Lookup finds a field by id.
func Lookup(id string) (Field, bool) {
	for _, f := range fields {
		if f.ID == id {
			return f, true
		}
	}
	return Field{}, false
}

// IsField reports whether id is an editable key.
func IsField(id string) bool {
	_, ok := Lookup(id)
	return ok
}

// Validate checks a value for a key; nil always passes (it removes the key).
func Validate(id string, value any) (ok bool, reason string) {
	f, known := Lookup(id)
	if !known {
		return false, "campo desconocido: " + id
	}
	if value == nil {
		return true, ""
	}
	for _, o := range f.Options {
		if o == value {
			return true, ""
		}
	}
	return false, fmt.Sprintf("valor inválido para %s: %v", id, value)
}

// SetPath is a copy of obj with value at path. A nil value removes the key and prunes the objects
// that end up empty (setPath of the 1.x).
func SetPath(obj map[string]any, path []string, value any) map[string]any {
	root := make(map[string]any, len(obj)+1)
	for k, v := range obj {
		root[k] = v
	}
	if len(path) == 0 {
		return root
	}
	head, rest := path[0], path[1:]
	if len(rest) == 0 {
		if value == nil {
			delete(root, head)
		} else {
			root[head] = value
		}
		return root
	}
	child, _ := root[head].(map[string]any)
	next := SetPath(child, rest, value)
	if value == nil && len(next) == 0 {
		delete(root, head)
	} else {
		root[head] = next
	}
	return root
}

// Files are the two config files, as decoded objects (nil when missing).
type Files struct {
	User, Repo map[string]any
}

// Source says where a key's value comes from.
type Source string

// The sources, as the 1.x names them.
const (
	SourceDefault Source = "default"
	SourceUser    Source = "user"
	SourceRepo    Source = "repo"
	SourceEnv     Source = "env"
)

// SourcesOf tells where the value of a key comes from: the environment (NXY_FILTER=0|1 for
// modules.filter, NXY_ENGINE=rtk|off for filter.engine) beats the repo file, which beats the user file.
func SourcesOf(id string, files Files, getenv func(string) string) Source {
	f, ok := Lookup(id)
	if !ok {
		return SourceDefault
	}
	if getenv != nil {
		if id == "modules.filter" {
			if v := getenv("NXY_FILTER"); v == "0" || v == "1" {
				return SourceEnv
			}
		}
		if id == "filter.engine" {
			if v := getenv("NXY_ENGINE"); v == "rtk" || v == "off" {
				return SourceEnv
			}
		}
	}
	if has(files.Repo, f.Path) {
		return SourceRepo
	}
	if has(files.User, f.Path) {
		return SourceUser
	}
	return SourceDefault
}

func has(obj map[string]any, path []string) bool {
	var cur any = obj
	for _, k := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return false
		}
		v, present := m[k]
		if !present {
			return false
		}
		cur = v
	}
	return true // a key saved as null counts as present, as in the 1.x
}

// RoleOf is the model and effort the config sets for a role (its name without the "nxy:" prefix).
// A missing, non-string or unknown value is "": the role keeps what its frontmatter says.
func RoleOf(cfg map[string]any, role string) (model, effort string) {
	roles, _ := cfg["roles"].(map[string]any)
	r, _ := roles[role].(map[string]any)
	m, _ := r["model"].(string)
	e, _ := r["effort"].(string)
	if !contains(Models, m) {
		m = ""
	}
	if !contains(Efforts, e) {
		e = ""
	}
	return m, e
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
