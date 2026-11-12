package views

import (
	"strings"

	"github.com/mananos/nxy.dev/internal/configedit"
)

// RoleIn is what the caller read for one role: the model and effort in force ("" when none is
// configured) and the layer they come from (default|user|repo|env).
type RoleIn struct {
	Role, Model, Effort, Source string
}

// SettingIn is the value in force of one editable key and where it comes from.
type SettingIn struct {
	ID     string
	Value  any
	Source string
}

// ConfigIn is the data of Config: the screen holds the fixed tables, the caller the values.
type ConfigIn struct {
	Layers     []Layer
	Roles      []RoleIn
	Settings   []SettingIn
	Statusline Statusline
	Tools      []Tool
	Binary     Binary
}

type roleInfo struct{ role, stage, does string }

// roleTable is the order of the roles on screen with what each one does.
var roleTable = []roleInfo{
	{"planner", "Planificar", "Lee el repo y escribe el plan con sus lotes y pruebas"},
	{"scout", "Planificar", "Explora el repo con búsquedas baratas para el planner"},
	{"implementer", "Implementar", "Escribe el código de un lote hasta que su prueba pasa"},
	{"reviewer", "Revisar", "Revisa el diff completo con lentes y marca hallazgos"},
	{"reviewer-quick", "Revisar", "Revisión corta para cambios chicos"},
	{"refuter", "Revisar", "Intenta refutar cada hallazgo antes de que te lo muestre"},
	{"documenter", "Documentar", "Actualiza la documentación de lo que cambió"},
}

var settingLabels = map[string]string{
	"modules.filter": "Filtro de salida",
	"filter.engine":  "Motor del filtro",
}

// BuildConfig joins the values read with the fixed tables (stage and "does" per role, the models and
// efforts configedit accepts) and the commands that change them.
func BuildConfig(in ConfigIn) Config {
	byRole := map[string]RoleIn{}
	for _, r := range in.Roles {
		byRole[r.Role] = r
	}
	c := Config{
		Layers:     append([]Layer{}, in.Layers...),
		Roles:      []RoleRow{},
		Settings:   []Setting{},
		Statusline: in.Statusline,
		Tools:      append([]Tool{}, in.Tools...),
		Binary:     in.Binary,
	}
	for _, info := range roleTable {
		r := byRole[info.role]
		src := r.Source
		if src == "" {
			src = string(configedit.SourceDefault)
		}
		c.Roles = append(c.Roles, RoleRow{
			Role: info.role, Stage: info.stage, Does: info.does,
			Model: r.Model, Effort: r.Effort, Source: src,
			Models:  append([]string{}, configedit.Models...),
			Efforts: append([]string{}, configedit.Efforts...),
			Actions: []Action{
				setAction("model", "Modelo", "roles."+info.role+".model", false),
				setAction("effort", "Esfuerzo", "roles."+info.role+".effort", false),
				setAction("model_repo", "Modelo en este repo", "roles."+info.role+".model", true),
				setAction("effort_repo", "Esfuerzo en este repo", "roles."+info.role+".effort", true),
			},
		})
	}
	vals := map[string]SettingIn{}
	for _, s := range in.Settings {
		vals[s.ID] = s
	}
	for _, f := range configedit.Fields() {
		if strings.HasPrefix(f.ID, "roles.") {
			continue
		}
		v := vals[f.ID]
		src := v.Source
		if src == "" {
			src = string(configedit.SourceDefault)
		}
		label := settingLabels[f.ID]
		if label == "" {
			label = f.ID
		}
		c.Settings = append(c.Settings, Setting{
			ID: f.ID, Label: label, Value: v.Value, Source: src,
			Options: append([]any{}, f.Options...),
			Actions: []Action{
				setAction("set", "Cambiar", f.ID, false),
				setAction("set_repo", "Cambiar en este repo", f.ID, true),
			},
		})
	}
	return c
}

func setAction(id, label, key string, repo bool) Action {
	argv := []string{"nxy", "config", "set", key, "{value}"}
	a := Action{ID: id, Label: label}
	if repo {
		argv = append(argv, "--repo")
		a.Confirm = "Guarda en .nxy/config.json del repo"
	} else {
		a.Confirm = "Guarda en ~/.nxy/config.json"
	}
	a.Argv = argv
	return a
}
