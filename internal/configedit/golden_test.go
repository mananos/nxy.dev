package configedit

import (
	"reflect"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/internal/goldentest"
)

func TestGolden(t *testing.T) {
	for _, c := range goldentest.Load(t, "configedit") {
		t.Run(c.Name, func(t *testing.T) {
			switch {
			case strings.HasPrefix(c.Name, "validate:"):
				var field string
				var value any
				c.In(t, "field", &field)
				c.In(t, "value", &value)
				ok, reason := Validate(field, value)
				got := map[string]any{"ok": ok}
				if !ok {
					got["reason"] = reason
				}
				goldentest.Check(t, c, got)
			case strings.HasPrefix(c.Name, "setpath:"):
				var obj map[string]any
				var path []string
				var value any
				c.In(t, "obj", &obj)
				c.In(t, "path", &path)
				c.In(t, "value", &value)
				goldentest.Check(t, c, map[string]any{"obj": SetPath(obj, path, value)})
			case strings.HasPrefix(c.Name, "sources:"):
				var field string
				var env map[string]string
				c.In(t, "field", &field)
				var raw struct {
					User map[string]any `json:"user"`
					Repo map[string]any `json:"repo"`
				}
				c.In(t, "files", &raw)
				files := Files{User: raw.User, Repo: raw.Repo}
				c.In(t, "env", &env)
				goldentest.Check(t, c, map[string]any{"source": string(SourcesOf(field, files, func(k string) string { return env[k] }))})
			default:
				t.Fatalf("unknown golden case %s", c.Name)
			}
		})
	}
}

func TestV2OnlyKeys(t *testing.T) {
	for _, id := range []string{"roles.reviewer-quick.model", "roles.refuter.effort", "filter.engine"} {
		if !IsField(id) {
			t.Errorf("%s should be a field", id)
		}
	}
	if ok, _ := Validate("filter.engine", "rtk"); !ok {
		t.Error("filter.engine=rtk should be valid")
	}
	if ok, reason := Validate("filter.engine", "x"); ok || reason != "valor inválido para filter.engine: x" {
		t.Errorf("filter.engine=x: %v %q", ok, reason)
	}
	if IsField("gate.enabled") {
		t.Error("gate.enabled is a 1.x key the v2 does not read")
	}
	get := func(k string) string {
		if k == "NXY_ENGINE" {
			return "off"
		}
		return ""
	}
	if s := SourcesOf("filter.engine", Files{}, get); s != SourceEnv {
		t.Errorf("NXY_ENGINE source = %s", s)
	}
}

func TestRoleOf(t *testing.T) {
	cfg := map[string]any{"roles": map[string]any{
		"scout":   map[string]any{"model": "haiku", "effort": "low"},
		"planner": map[string]any{"model": 5.0, "effort": "extreme"},
		"refuter": map[string]any{"effort": "high"},
	}}
	cases := []struct{ role, model, effort string }{
		{"scout", "haiku", "low"},
		{"planner", "", ""},
		{"refuter", "", "high"},
		{"reviewer", "", ""},
	}
	for _, c := range cases {
		m, e := RoleOf(cfg, c.role)
		if m != c.model || e != c.effort {
			t.Errorf("%s: %q %q", c.role, m, e)
		}
	}
	if m, e := RoleOf(nil, "scout"); m != "" || e != "" {
		t.Error("nil config")
	}
}

func TestSetPathDoesNotMutate(t *testing.T) {
	in := map[string]any{"a": map[string]any{"b": 1.0}}
	_ = SetPath(in, []string{"a", "b"}, nil)
	if !reflect.DeepEqual(in, map[string]any{"a": map[string]any{"b": 1.0}}) {
		t.Errorf("input changed: %v", in)
	}
}
