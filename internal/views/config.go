package views

// Config is the config screen: layers, roles, modules, statusline, tools and binary.
type Config struct {
	Layers     []Layer    `json:"layers"`
	Roles      []RoleRow  `json:"roles"`
	Settings   []Setting  `json:"settings"`
	Statusline Statusline `json:"statusline"`
	Tools      []Tool     `json:"tools"`
	Binary     Binary     `json:"binary"`
	// Warnings are config values that are not valid and nxy ignores.
	Warnings []string `json:"warnings"`
}

// Layer is a config file (user or repo).
type Layer struct {
	Name   string `json:"name"` // user|repo
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
}

// RoleRow is the model and effort of a subagent role.
type RoleRow struct {
	Role    string   `json:"role"`
	Stage   string   `json:"stage"` // Planificar|Implementar|Revisar|Documentar
	Does    string   `json:"does"`  // what the role does, one short line
	Model   string   `json:"model"`
	Effort  string   `json:"effort"`
	Source  string   `json:"source"` // default|user|repo
	Models  []string `json:"models"`
	Efforts []string `json:"efforts"`
	// Actions are ids model, effort (saved for the user) and model_repo, effort_repo (saved in the
	// repo). Each Argv carries {value}.
	Actions []Action `json:"actions"`
}

// Setting is one module switch or option.
type Setting struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	Value   any      `json:"value"`
	Source  string   `json:"source"`
	Options []any    `json:"options"`
	Actions []Action `json:"actions"`
}

// Statusline says whether the statusline is installed in settings.json.
type Statusline struct {
	Installed    bool   `json:"installed"`
	SettingsPath string `json:"settings_path"`
	Install      Action `json:"install"`
	Remove       Action `json:"remove"`
}

// Tool is an optional external tool.
type Tool struct {
	Name    string  `json:"name"` // rtk|rg|codegraph|d2
	Found   bool    `json:"found"`
	Version string  `json:"version"`
	Path    string  `json:"path"`
	Install *Action `json:"install"`
}

// Binary compares the installed binary with the one the plugin wants.
type Binary struct {
	Installed     string `json:"installed"`
	Wanted        string `json:"wanted"`
	Path          string `json:"path"`
	Download      string `json:"download"` // none|running|failed
	ManualCommand string `json:"manual_command"`
}
