package views

import (
	"reflect"
	"testing"
)

func TestBuildConfig(t *testing.T) {
	c := BuildConfig(ConfigIn{
		InRepo:   true,
		Roles:    []RoleIn{{Role: "planner", Model: "opus", Effort: "high", Source: "user"}, {Role: "scout", Model: "haiku", Source: "repo"}},
		Settings: []SettingIn{{ID: "modules.filter", Value: false, Source: "env"}},
	})
	var order []string
	for _, r := range c.Roles {
		order = append(order, r.Role)
	}
	want := []string{"planner", "scout", "implementer", "reviewer", "reviewer-quick", "refuter", "documenter"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("roles %v", order)
	}
	p := c.Roles[0]
	if p.Stage != "Planificar" || p.Model != "opus" || p.Source != "user" || len(p.Models) != 4 || len(p.Efforts) != 5 {
		t.Errorf("planner %+v", p)
	}
	if im := c.Roles[2]; im.Source != "default" || im.Model != "" || im.Stage != "Implementar" {
		t.Errorf("implementer %+v", im)
	}
	var ids []string
	for _, a := range p.Actions {
		ids = append(ids, a.ID)
	}
	if !reflect.DeepEqual(ids, []string{"model", "effort", "model_repo", "effort_repo"}) {
		t.Errorf("actions %v", ids)
	}
	if got := p.Actions[2].Argv; !reflect.DeepEqual(got, []string{"nxy", "config", "set", "roles.planner.model", "{value}", "--repo"}) {
		t.Errorf("model_repo %v", got)
	}
	if len(c.Settings) != 2 || c.Settings[0].ID != "modules.filter" || c.Settings[0].Source != "env" || c.Settings[0].Value != false {
		t.Fatalf("settings %+v", c.Settings)
	}
	if c.Settings[1].Source != "default" || len(c.Settings[1].Options) != 3 {
		t.Errorf("engine %+v", c.Settings[1])
	}
	if c.Layers == nil || c.Tools == nil {
		t.Error("lists must not be nil")
	}
}

func TestBuildConfigCarriesWarnings(t *testing.T) {
	c := BuildConfig(ConfigIn{Warnings: []string{"roles.scout.effort: valor inválido"}})
	if !reflect.DeepEqual(c.Warnings, []string{"roles.scout.effort: valor inválido"}) {
		t.Fatalf("warnings %v", c.Warnings)
	}
	if c := BuildConfig(ConfigIn{}); c.Warnings == nil || len(c.Warnings) != 0 {
		t.Fatalf("no warnings must be an empty list, got %#v", c.Warnings)
	}
}

func TestBuildConfigOutsideRepo(t *testing.T) {
	c := BuildConfig(ConfigIn{
		InRepo: false,
		Layers: []Layer{{Name: "user", Path: "u"}, {Name: "repo", Path: "r"}},
	})
	if len(c.Layers) != 1 || c.Layers[0].Name != "user" {
		t.Errorf("layers %+v", c.Layers)
	}
	for _, r := range c.Roles {
		for _, a := range r.Actions {
			if a.ID == "model_repo" || a.ID == "effort_repo" {
				t.Errorf("%s has %s", r.Role, a.ID)
			}
		}
	}
	for _, s := range c.Settings {
		for _, a := range s.Actions {
			if a.ID == "set_repo" {
				t.Errorf("%s has set_repo", s.ID)
			}
		}
	}
}
