// Package setup plans the install of the external tools nxy leans on (rtk, rg, codegraph, d2) and
// words what `nxy setup` prints. Port of core/setup.mjs and hosts/claude-code/entries/setup.mjs.
//
// Pure: planning never touches the machine; Run only calls the Spawn it is given. Platform names
// are the ones of the 1.x (process.platform: "win32", "darwin", "linux"), so the golden of the 1.x
// compares as is and `nxy setup status` prints the same header.
package setup

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/mananos/nxy.dev/v2/internal/filter"
	"github.com/mananos/nxy.dev/v2/internal/jscompat"
)

// Tools are the tools `nxy setup` knows, in the order it lists them.
var Tools = []string{"rtk", "rg", "codegraph", "d2"}

const (
	d2Sh  = "https://d2lang.com/install.sh"
	rtkSh = "https://raw.githubusercontent.com/rtk-ai/rtk/refs/heads/master/install.sh"
	cgSh  = "https://raw.githubusercontent.com/colbymchenry/codegraph/main/install.sh"
	cgPs1 = "https://raw.githubusercontent.com/colbymchenry/codegraph/main/install.ps1"
)

// Step is one command of a plan, run without a shell.
type Step struct {
	Display   string   `json:"display"`
	File      string   `json:"file"`
	Args      []string `json:"args"`
	Source    string   `json:"source"`
	NeedsSudo bool     `json:"needsSudo"`
}

// Plan is what installing (or reinstalling) one tool runs. Manual lists what only the user can do.
type Plan struct {
	Tool    string   `json:"tool"`
	Action  string   `json:"action"`
	OS      string   `json:"os"`
	Steps   []Step   `json:"steps"`
	Removes []Step   `json:"removes"`
	Manual  []string `json:"manual"`
}

// Conflict values: "" none, ConflictYes (rtk's own hook, or loose settings hooks), "mcp", "hooks".
const (
	ConflictYes   = "yes"
	ConflictMCP   = "mcp"
	ConflictHooks = "hooks"
)

// Opts feed InstallPlan. RtkPath nil means "not given" (rtk when found, none otherwise); a pointer
// to "" means no rtk binary. HookScopes nil means ["user"]; an empty non-nil slice stays empty.
type Opts struct {
	Platform   string
	PM         string
	Found      bool
	Conflict   string
	RtkPath    *string
	HookScopes []string
	MCPScope   string
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func commandsOf(settings any, event string) []string {
	var out []string
	groups, _ := asMap(asMap(settings)["hooks"])[event].([]any)
	for _, g := range groups {
		hs, _ := asMap(g)["hooks"].([]any)
		for _, h := range hs {
			if c, ok := asMap(h)["command"].(string); ok {
				out = append(out, c)
			}
		}
	}
	return out
}

// DetectRtkConflict is true when rtk's own hook is wired in any of the parsed settings objects.
func DetectRtkConflict(settings []any) bool {
	var cmds []string
	for _, s := range settings {
		cmds = append(cmds, commandsOf(s, "PreToolUse")...)
	}
	return filter.RtkHookIn(cmds)
}

// DetectCodegraphMcp is true when one parsed config object declares an mcpServers.codegraph entry.
func DetectCodegraphMcp(obj any) bool {
	_, ok := asMap(asMap(obj)["mcpServers"])["codegraph"]
	return ok
}

var codegraphWord = regexp.MustCompile(`(?i)codegraph`)

// DetectCodegraphWiring is true when `codegraph install` left its wiring: an mcpServers.codegraph
// entry or a hook whose command names codegraph.
func DetectCodegraphWiring(settings, mcp []any) bool {
	for _, o := range append(append([]any{}, settings...), mcp...) {
		if DetectCodegraphMcp(o) {
			return true
		}
	}
	for _, s := range settings {
		hooks := asMap(asMap(s)["hooks"])
		for event := range hooks {
			for _, c := range commandsOf(s, event) {
				if codegraphWord.MatchString(c) {
					return true
				}
			}
		}
	}
	return false
}

// PackageManager is the package manager of the platform, or "". has says whether a binary is on PATH.
func PackageManager(platform string, has func(bin string) bool) string {
	switch platform {
	case "win32":
		if has("winget") {
			return "winget"
		}
		return ""
	case "darwin":
		if has("brew") {
			return "brew"
		}
		return ""
	}
	for _, pm := range []string{"apt-get", "dnf", "pacman", "brew"} {
		if has(pm) {
			return pm
		}
	}
	return ""
}

// isJSSpace is JavaScript's \s.
func isJSSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return r >= 0x2000 && r <= 0x200a
}

func quoteArg(a string) string {
	if strings.IndexFunc(a, isJSSpace) >= 0 {
		return `"` + a + `"`
	}
	return a
}

// The script is downloaded to a temp file first so a failed curl fails the step (a plain
// `curl | sh` exits with sh's status). The displayed command is the executed one.
func shStep(url, source string) Step {
	script := fmt.Sprintf(`t=$(mktemp) && curl -fsSL %s -o "$t" && sh "$t"; r=$?; rm -f "$t"; exit $r`, url)
	return Step{Display: script, File: "sh", Args: []string{"-c", script}, Source: source}
}

// shStepArgs is shStep with arguments for the downloaded script (`sh "$t" args`).
func shStepArgs(url, source, args string) Step {
	script := fmt.Sprintf(`t=$(mktemp) && curl -fsSL %s -o "$t" && sh "$t" %s; r=$?; rm -f "$t"; exit $r`, url, args)
	return Step{Display: script, File: "sh", Args: []string{"-c", script}, Source: source}
}

func cmdStep(file string, args []string, source string) Step {
	parts := append([]string{file}, args...)
	for i, p := range parts {
		parts[i] = quoteArg(p)
	}
	return Step{Display: strings.Join(parts, " "), File: file, Args: args, Source: source}
}

func sudoStep(file string, args []string, source string) Step {
	full := append([]string{"-n", file}, args...)
	return Step{Display: "sudo " + strings.Join(full, " "), File: "sudo", Args: full, Source: source, NeedsSudo: true}
}

func installOrUpgrade(found bool) string {
	if found {
		return "upgrade"
	}
	return "install"
}

func rgSteps(os, pm string, found bool) []Step {
	const src = "README.md"
	if os == "win32" {
		return []Step{cmdStep("winget", []string{installOrUpgrade(found), "BurntSushi.ripgrep.MSVC"}, src)}
	}
	switch pm {
	case "brew":
		return []Step{cmdStep("brew", []string{installOrUpgrade(found), "ripgrep"}, src)}
	case "apt-get":
		return []Step{sudoStep("apt-get", []string{"install", "-y", "ripgrep"}, src)}
	case "dnf":
		return []Step{sudoStep("dnf", []string{"install", "-y", "ripgrep"}, src)}
	case "pacman":
		return []Step{sudoStep("pacman", []string{"-S", "--noconfirm", "ripgrep"}, src)}
	}
	return []Step{}
}

func stepsFor(tool, os, pm string, found bool) []Step {
	switch tool {
	case "rtk":
		switch os {
		case "win32":
			return []Step{cmdStep("winget", []string{installOrUpgrade(found), "rtk-ai.rtk"}, "README.md")}
		case "darwin":
			return []Step{cmdStep("brew", []string{installOrUpgrade(found), "rtk"}, "README.md")}
		}
		return []Step{shStep(rtkSh, "github.com/rtk-ai/rtk install.sh")}
	case "rg":
		return rgSteps(os, pm, found)
	case "d2":
		switch os {
		case "win32":
			return []Step{cmdStep("winget", []string{installOrUpgrade(found), "Terrastruct.D2"}, "d2lang.com")}
		case "darwin":
			return []Step{cmdStep("brew", []string{installOrUpgrade(found), "d2"}, "d2lang.com")}
		}
		// no sudo: the official script installs under ~/.local
		return []Step{shStepArgs(d2Sh, "d2lang.com install.sh", `--prefix "$HOME/.local"`)}
	case "codegraph":
		if os == "win32" {
			// Stop makes a failed download a terminating error, hence a non-zero exit.
			cmd := fmt.Sprintf(`$ErrorActionPreference='Stop'; irm %s | iex`, cgPs1)
			return []Step{{
				Display: fmt.Sprintf(`powershell -NoProfile -Command "%s"`, cmd),
				File:    "powershell",
				Args:    []string{"-NoProfile", "-Command", cmd},
				Source:  "github.com/colbymchenry/codegraph install.ps1",
			}}
		}
		return []Step{shStep(cgSh, "github.com/colbymchenry/codegraph install.sh")}
	}
	return []Step{}
}

func contains(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
}

// InstallPlan builds the plan for one tool. Conflict is rtk's own hook for rtk; for codegraph it is
// "mcp" (registered MCP server) or anything else for loose settings hooks. HookScopes says where
// rtk's hook lives ("user" and/or "project"); MCPScope is where the codegraph MCP was found
// ("user", "project" or "local").
func InstallPlan(tool string, o Opts) Plan {
	hookScopes := o.HookScopes
	if hookScopes == nil {
		hookScopes = []string{"user"}
	}
	var rtkPath string
	switch {
	case o.RtkPath != nil:
		rtkPath = *o.RtkPath
	case o.Found:
		rtkPath = "rtk"
	}
	steps := stepsFor(tool, o.Platform, o.PM, o.Found)
	removes := []Step{}
	manual := []string{}
	if o.Conflict != "" {
		switch tool {
		case "rtk":
			// `rtk init --help` lists --uninstall ("Remove RTK artifacts"); -g targets the global settings only.
			if contains(hookScopes, "user") {
				if rtkPath != "" {
					removes = append(removes, cmdStep(rtkPath, []string{"init", "-g", "--uninstall"}, "rtk init --help"))
				} else {
					manual = append(manual, "rtk no se encontró, así que no se puede quitar su hook: una vez instalado, corré `rtk init -g --uninstall` en una terminal.")
				}
			}
			if contains(hookScopes, "project") {
				manual = append(manual, "El hook de rtk está en los settings del proyecto (.claude/settings*.json): quitalo a mano (-g no lo toca; nxy no edita settings ajenos).")
			}
		case "codegraph":
			if o.Conflict == ConflictMCP {
				if o.MCPScope != "" {
					removes = append(removes, cmdStep("claude", []string{"mcp", "remove", "codegraph", "-s", o.MCPScope}, "claude mcp"))
				} else {
					manual = append(manual, "Quitá a mano el servidor MCP codegraph (`claude mcp remove codegraph`, o de tus settings): no se pudo saber en qué alcance está.")
				}
			} else {
				manual = append(manual, "Quitá a mano de tus settings el hook cuyo comando nombra codegraph (nxy no edita settings ajenos).")
			}
		}
	}
	for _, s := range steps {
		if !s.NeedsSudo {
			continue
		}
		rest := s.Display
		if s.File == "sudo" {
			rest = strings.Join(s.Args[1:], " ")
		}
		manual = append(manual, "sudo "+rest)
	}
	action := "install"
	if o.Found {
		action = "reinstall"
	}
	return Plan{Tool: tool, Action: action, OS: o.Platform, Steps: steps, Removes: removes, Manual: manual}
}

// DescribePlan gives readable lines: exact command and source per step; sudo steps are flagged. The
// "a mano" notes are only included with manual (they are not part of what will run).
func DescribePlan(p Plan, manual bool) []string {
	var lines []string
	for _, r := range p.Removes {
		lines = append(lines, fmt.Sprintf("quita:   %s   (%s)", r.Display, r.Source))
	}
	if len(p.Steps) == 0 {
		lines = append(lines, fmt.Sprintf("%s: sin gestor de paquetes conocido para %s en %s", p.Action, p.Tool, p.OS))
	}
	for _, s := range p.Steps {
		sudo := ""
		if s.NeedsSudo {
			sudo = "   [sudo -n: sin contraseña en caché falla]"
		}
		lines = append(lines, fmt.Sprintf("%s: %s   (fuente: %s)%s", p.Action, s.Display, s.Source, sudo))
	}
	if manual {
		for _, m := range p.Manual {
			lines = append(lines, "a mano:  "+m)
		}
	}
	return lines
}

// sudo -n output when it would need a password or a terminal (English and Spanish locales).
var sudoNoTTY = regexp.MustCompile(`(?i)password is required|terminal is required|no tty present|se requiere una (contrase|terminal)|requiere[^\n\r\x{2028}\x{2029}]{0,20}(contrase|terminal)`)

// IsSudoNoTTY is true when sudo -n failed for lack of a password or terminal.
func IsSudoNoTTY(s string) bool { return sudoNoTTY.MatchString(s) }

// Clip keeps the last n UTF-16 units of s behind an ellipsis when s is longer.
func Clip(s string, n int) string {
	if jscompat.UTF16Len(s) <= n {
		return s
	}
	rs := []rune(s)
	units, i := 0, len(rs)
	for i > 0 {
		w := 1
		if rs[i-1] > 0xffff {
			w = 2
		}
		if units+w > n {
			break
		}
		units += w
		i--
	}
	return "…" + string(rs[i:])
}

// SpawnResult is what running one step gave. Err is non-empty when the process could not run or
// timed out; Status is its exit code.
type SpawnResult struct {
	Stdout string
	Stderr string
	Err    string
	Status int
}

// Spawn runs one step.
type Spawn func(Step) SpawnResult

// RunResult is how a plan ended.
type RunResult struct {
	OK       bool
	Ran      []string
	FailedAt string
	Tail     string
	Manual   []string
}

// Run runs removes then steps in order and stops at the first failure.
func Run(p Plan, spawn Spawn) RunResult {
	ran := []string{}
	tail := ""
	all := append(append([]Step{}, p.Removes...), p.Steps...)
	for _, s := range all {
		ran = append(ran, s.Display)
		r := spawn(s)
		tail = Clip(jscompat.Trim(r.Stdout+r.Stderr+r.Err), 1500)
		if r.Err != "" || r.Status != 0 {
			if s.NeedsSudo && IsSudoNoTTY(tail) {
				tail = Clip(jscompat.Trim(tail+"\nsudo no pudo pedir contraseña sin terminal. Pegá en una terminal: "+strings.Join(p.Manual, " ; ")), 1500)
			}
			return RunResult{OK: false, Ran: ran, FailedAt: s.Display, Tail: tail, Manual: p.Manual}
		}
	}
	return RunResult{OK: true, Ran: ran, Tail: tail, Manual: []string{}}
}

// ToolInfo is what nxy knows of one tool on this machine.
type ToolInfo struct {
	Found    bool
	Path     string // "" when unknown
	Version  string // "" when unknown
	OnPath   bool
	Conflict string // "", ConflictYes, ConflictMCP or ConflictHooks
	Plan     Plan
}

// MarshalJSON writes the shape of the 1.x: path and version null when unknown, conflict false or a
// string (true for rtk's hook).
func (i ToolInfo) MarshalJSON() ([]byte, error) {
	str := func(s string) *string {
		if s == "" {
			return nil
		}
		return &s
	}
	var conflict any = false
	switch i.Conflict {
	case "":
	case ConflictYes:
		conflict = true
	default:
		conflict = i.Conflict
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false) // the commands carry & and >, which JSON.stringify leaves alone
	err := enc.Encode(struct {
		Found    bool    `json:"found"`
		Path     *string `json:"path"`
		Version  *string `json:"version"`
		OnPath   bool    `json:"onPath"`
		Conflict any     `json:"conflict"`
		Plan     Plan    `json:"plan"`
	}{i.Found, str(i.Path), str(i.Version), i.OnPath, conflict, i.Plan})
	return bytes.TrimSuffix(b.Bytes(), []byte("\n")), err
}

// State is the detection of the tools (hosts/claude-code/setup-state.mjs toolsState, plus d2).
type State struct {
	Platform  string
	Rtk       ToolInfo
	Rg        ToolInfo
	Codegraph ToolInfo
	D2        ToolInfo
}

// Tool returns the info of one of Tools.
func (s State) Tool(name string) ToolInfo {
	switch name {
	case "rtk":
		return s.Rtk
	case "rg":
		return s.Rg
	case "d2":
		return s.D2
	}
	return s.Codegraph
}

// StatusJSON is `nxy setup status --json`, one line.
func StatusJSON(s State) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(struct {
		OK       bool   `json:"ok"`
		Platform string `json:"platform"`
		Tools    struct {
			Rtk       ToolInfo `json:"rtk"`
			Rg        ToolInfo `json:"rg"`
			Codegraph ToolInfo `json:"codegraph"`
			D2        ToolInfo `json:"d2"`
		} `json:"tools"`
	}{OK: true, Platform: s.Platform, Tools: struct {
		Rtk       ToolInfo `json:"rtk"`
		Rg        ToolInfo `json:"rg"`
		Codegraph ToolInfo `json:"codegraph"`
		D2        ToolInfo `json:"d2"`
	}{s.Rtk, s.Rg, s.Codegraph, s.D2}})
	return strings.TrimSuffix(b.String(), "\n")
}

// StatusText is `nxy setup status`.
func StatusText(s State) string {
	lines := []string{fmt.Sprintf("nxy setup (%s)", s.Platform)}
	for _, t := range Tools {
		i := s.Tool(t)
		where := "no encontrada"
		switch {
		case !i.Found:
		case i.OnPath:
			where = "en el PATH"
		default:
			where = "sólo en un directorio conocido (una terminal nueva la ve en el PATH)"
		}
		found := "no encontrada"
		if i.Found {
			found = "encontrada"
			if i.Version != "" {
				found += " " + i.Version
			}
			if i.Path != "" {
				found += " (" + i.Path + ")"
			}
		}
		lines = append(lines, fmt.Sprintf("\n%s: %s — %s", t, found, where))
		if i.Conflict != "" {
			what := "hook de codegraph en tus settings"
			switch {
			case t == "rtk":
				what = "hook propio de rtk en tus settings"
			case i.Conflict == ConflictMCP:
				what = "codegraph ya registrado como servidor MCP"
			}
			lines = append(lines, "  choca: "+what)
		}
		for _, l := range DescribePlan(i.Plan, true) {
			lines = append(lines, "  "+l)
		}
	}
	lines = append(lines, "\nNada se instaló. Para instalar: nxy setup run <herramienta> --yes")
	return strings.Join(lines, "\n")
}

// Usage lines of `nxy setup`.
const (
	StatusUsage = "uso: nxy setup status [--json] | run <rtk|rg|codegraph|d2> --yes [--dry-run]"
	RunUsage    = "uso: nxy setup run <rtk|rg|codegraph|d2> --yes [--dry-run]"
)

// MissingYesText is what `run` prints without --yes (exit 1).
func MissingYesText(tool string, p Plan) string {
	lines := []string{fmt.Sprintf("Falta --yes: no se instala nada. Lo que correría para %s:", tool)}
	for _, l := range DescribePlan(p, true) {
		lines = append(lines, "  "+l)
	}
	return strings.Join(lines, "\n")
}

// RunHeader is what `run --yes` prints before running (or, with dryRun, instead of running).
func RunHeader(tool string, p Plan, dryRun bool) string {
	head := fmt.Sprintf("%s %s:", p.Action, tool)
	if dryRun {
		head = fmt.Sprintf("%s %s (dry-run: no se ejecuta nada):", p.Action, tool)
	}
	lines := []string{head}
	for _, l := range DescribePlan(p, false) {
		lines = append(lines, "  "+l)
	}
	if dryRun {
		for _, m := range p.Manual {
			lines = append(lines, "a mano: "+m)
		}
	}
	return strings.Join(lines, "\n")
}

// RunOutcome is what `run --yes` prints after the plan ran; the second value is the exit code.
func RunOutcome(tool string, p Plan, r RunResult) (string, int) {
	var lines []string
	if r.Tail != "" {
		lines = append(lines, r.Tail)
	}
	if !r.OK {
		lines = append(lines, "Falló en: "+r.FailedAt)
		if len(r.Manual) > 0 {
			lines = append(lines, "Para correrlo en una terminal: "+strings.Join(r.Manual, " ; "))
		}
		return strings.Join(lines, "\n"), 1
	}
	plural := "s"
	if len(r.Ran) == 1 {
		plural = ""
	}
	lines = append(lines, fmt.Sprintf("%s: listo (%d paso%s).", tool, len(r.Ran), plural))
	// sudo fallbacks (plain `sudo ...`) belong to steps that just ran OK; only the other notes still apply
	for _, m := range p.Manual {
		if !strings.HasPrefix(m, "sudo ") {
			lines = append(lines, "a mano: "+m)
		}
	}
	return strings.Join(lines, "\n"), 0
}
