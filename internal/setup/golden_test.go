package setup

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/v2/internal/goldentest"
)

type optsIn struct {
	Platform   string          `json:"platform"`
	PM         *string         `json:"pm"`
	Found      bool            `json:"found"`
	Conflict   json.RawMessage `json:"conflict"`
	RtkPath    json.RawMessage `json:"rtkPath"`
	HookScopes []string        `json:"hookScopes"`
	MCPScope   *string         `json:"mcpScope"`
}

func (o optsIn) opts(t *testing.T) Opts {
	t.Helper()
	out := Opts{Platform: o.Platform, Found: o.Found, HookScopes: o.HookScopes}
	if o.PM != nil {
		out.PM = *o.PM
	}
	if o.MCPScope != nil {
		out.MCPScope = *o.MCPScope
	}
	switch s := strings.TrimSpace(string(o.Conflict)); {
	case s == "" || s == "null" || s == "false":
	case s == "true":
		out.Conflict = ConflictYes
	default:
		if err := json.Unmarshal(o.Conflict, &out.Conflict); err != nil {
			t.Fatal(err)
		}
	}
	if o.RtkPath != nil {
		var p string
		if string(o.RtkPath) != "null" {
			if err := json.Unmarshal(o.RtkPath, &p); err != nil {
				t.Fatal(err)
			}
		}
		out.RtkPath = &p
	}
	return out
}

// scriptedIn is one scripted spawn result of the corpus. An absent status is exit 0; null is a
// process that was killed or never ran.
type scriptedIn struct {
	Stdout       string                    `json:"stdout"`
	Stderr       string                    `json:"stderr"`
	Status       json.RawMessage           `json:"status"`
	Error        *struct{ Message string } `json:"error"`
	StdoutRepeat []any                     `json:"stdoutRepeat"`
}

func (r scriptedIn) result(t *testing.T) SpawnResult {
	t.Helper()
	out := SpawnResult{Stdout: r.Stdout, Stderr: r.Stderr}
	if len(r.StdoutRepeat) == 2 {
		out.Stdout = strings.Repeat(r.StdoutRepeat[0].(string), int(r.StdoutRepeat[1].(float64)))
	}
	if r.Error != nil {
		out.Err = r.Error.Message
	}
	switch s := string(r.Status); s {
	case "":
	case "null":
		out.Status = -1
	default:
		if err := json.Unmarshal(r.Status, &out.Status); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func TestGolden(t *testing.T) {
	for _, c := range goldentest.Load(t, "setup") {
		t.Run(c.File+"/"+c.Name, func(t *testing.T) {
			switch c.File {
			case "plan.json":
				var tool string
				var o optsIn
				c.In(t, "tool", &tool)
				c.In(t, "opts", &o)
				p := InstallPlan(tool, o.opts(t))
				describe := DescribePlan(p, false)
				got := map[string]any{"plan": p, "describe": describe}
				if m := DescribePlan(p, true); len(m) != len(describe) {
					got["describeManual"] = m
				}
				goldentest.Check(t, c, got)
			case "pm.json":
				var platform string
				var has []string
				c.In(t, "platform", &platform)
				c.In(t, "has", &has)
				pm := PackageManager(platform, func(b string) bool {
					for _, h := range has {
						if h == b {
							return true
						}
					}
					return false
				})
				var got any
				if pm != "" {
					got = pm
				}
				goldentest.Check(t, c, map[string]any{"pm": got})
			case "detect.json":
				var settings, mcp []any
				var obj any
				c.In(t, "settings", &settings)
				c.In(t, "mcp", &mcp)
				c.In(t, "obj", &obj)
				goldentest.Check(t, c, map[string]any{
					"rtk": DetectRtkConflict(settings), "mcp": DetectCodegraphMcp(obj), "wiring": DetectCodegraphWiring(settings, mcp),
				})
			case "sudo.json":
				var text string
				c.In(t, "text", &text)
				plan := InstallPlan("rg", Opts{Platform: "linux", PM: "apt-get"})
				r := Run(plan, func(Step) SpawnResult { return SpawnResult{Stderr: text, Status: 1} })
				viaRun := strings.Contains(r.Tail, "sudo no pudo pedir contraseña")
				if viaRun != IsSudoNoTTY(strings.TrimSpace(text)) {
					t.Errorf("IsSudoNoTTY(%q) disagrees with Run", text)
				}
				goldentest.Check(t, c, map[string]any{"sudo": viaRun})
			case "run.json":
				var tool string
				var o optsIn
				var results []scriptedIn
				c.In(t, "tool", &tool)
				c.In(t, "opts", &o)
				c.In(t, "results", &results)
				i := 0
				r := Run(InstallPlan(tool, o.opts(t)), func(Step) SpawnResult {
					var s scriptedIn
					if i < len(results) {
						s = results[i]
					}
					i++
					return s.result(t)
				})
				var failedAt any
				if !r.OK {
					failedAt = r.FailedAt
				}
				goldentest.Check(t, c, map[string]any{"ok": r.OK, "ran": r.Ran, "failedAt": failedAt, "tail": r.Tail, "manual": r.Manual})
			default:
				t.Fatalf("unknown golden file %s", c.File)
			}
		})
	}
}
