package filter

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

type goldenCase struct {
	File   string
	Name   string                     `json:"name"`
	Input  map[string]json.RawMessage `json:"input"`
	Output map[string]json.RawMessage `json:"output"`
}

func loadGolden(t *testing.T, area string) []goldenCase {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", "testdata", "golden", area, "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no golden files for %s (err=%v)", area, err)
	}
	sort.Strings(files)
	var all []goldenCase
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var cases []goldenCase
		if err := json.Unmarshal(raw, &cases); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		for i := range cases {
			cases[i].File = filepath.Base(f)
		}
		all = append(all, cases...)
	}
	return all
}

func (c goldenCase) str(t *testing.T, key string) string {
	t.Helper()
	var s string
	if raw, ok := c.Input[key]; ok {
		if err := json.Unmarshal(raw, &s); err != nil {
			t.Fatalf("input.%s: %v", key, err)
		}
	}
	return s
}

// norm round-trips a value through JSON so it compares with decoded golden output.
func norm(t *testing.T, v any) any {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func checkOutput(t *testing.T, c goldenCase, got map[string]any) {
	t.Helper()
	for key, raw := range c.Output {
		var want any
		if err := json.Unmarshal(raw, &want); err != nil {
			t.Fatal(err)
		}
		g, ok := got[key]
		if !ok {
			t.Fatalf("golden %s has output %q the port does not compute", c.File, key)
		}
		if !reflect.DeepEqual(want, norm(t, g)) {
			in, _ := json.Marshal(c.Input)
			t.Errorf("%s: %s\n input: %s\n want: %s\n  got: %s", c.File, key, in, raw, mustJSON(g))
		}
	}
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestGoldenShell(t *testing.T) {
	for _, c := range loadGolden(t, "shell") {
		t.Run(c.File+"/"+c.Name, func(t *testing.T) {
			cmd := c.str(t, "command")
			tokens := []string{}
			for _, s := range SplitTopLevel(cmd) {
				tokens = append(tokens, FirstToken(s))
			}
			checkOutput(t, c, map[string]any{
				"splitTopLevel":      SplitTopLevel(cmd),
				"hasHeredoc":         HasHeredoc(cmd),
				"hasSubstitution":    HasSubstitution(cmd),
				"hasRedirect":        HasRedirect(cmd),
				"stripTrailingLimit": StripTrailingLimit(cmd),
				"isBackgrounded":     IsBackgrounded(cmd),
				"firstToken":         FirstToken(cmd),
				"firstTokens":        tokens,
				"leadingAssignments": LeadingAssignments(cmd),
			})
		})
	}
}

func TestGoldenRtkCmd(t *testing.T) {
	for _, c := range loadGolden(t, "rtkcmd") {
		t.Run(c.File+"/"+c.Name, func(t *testing.T) {
			got := map[string]any{}
			if _, ok := c.Input["settings"]; ok {
				type group struct {
					Hooks []struct {
						Command any `json:"command"`
					} `json:"hooks"`
				}
				var in []*struct {
					Hooks struct {
						PreToolUse []group `json:"PreToolUse"`
					} `json:"hooks"`
				}
				if err := json.Unmarshal(c.Input["settings"], &in); err != nil {
					t.Fatal(err)
				}
				cmds := []string{}
				for _, s := range in {
					if s == nil {
						continue
					}
					for _, g := range s.Hooks.PreToolUse {
						for _, h := range g.Hooks {
							if s, ok := h.Command.(string); ok {
								cmds = append(cmds, s)
							}
						}
					}
				}
				got["detectRtkHook"] = RtkHookIn(cmds)
			} else if _, ok := c.Output["pinRtkPath"]; ok {
				got["pinRtkPath"] = PinRtkPath(c.str(t, "command"), c.str(t, "rtkPath"))
			} else {
				cmd := c.str(t, "command")
				got["normalizeForRtk"] = NormalizeForRtk(cmd)
				got["isRtkInvocation"] = IsRtkInvocation(cmd)
				got["usesRtk"] = UsesRtk(cmd)
			}
			checkOutput(t, c, got)
		})
	}
}

func TestGoldenDecide(t *testing.T) {
	for _, c := range loadGolden(t, "filter") {
		t.Run(c.File+"/"+c.Name, func(t *testing.T) {
			var ctxIn struct {
				Engine          string   `json:"engine"`
				RtkHookDetected bool     `json:"rtkHookDetected"`
				ExcludeCommands []string `json:"excludeCommands"`
				OnlyCommands    []string `json:"onlyCommands"`
			}
			if raw, ok := c.Input["ctx"]; ok {
				if err := json.Unmarshal(raw, &ctxIn); err != nil {
					t.Fatal(err)
				}
			}
			ctx := Context{
				Tool: c.str(t, "tool"), Engine: ctxIn.Engine, RtkHookDetected: ctxIn.RtkHookDetected,
				ExcludeCommands: ctxIn.ExcludeCommands, OnlyCommands: ctxIn.OnlyCommands,
			}
			var entry struct {
				Code   int    `json:"code"`
				Stdout string `json:"stdout"`
			}
			throw := false
			hasRtk := false
			if raw, ok := c.Input["rtk"]; ok && string(raw) != "null" {
				hasRtk = true
				if string(raw) == `"throw"` {
					throw = true
				} else if err := json.Unmarshal(raw, &entry); err != nil {
					t.Fatal(err)
				}
			}
			var asked any // nil until the port asks the engine
			if hasRtk {
				ctx.Rewrite = func(cmd string) (RewriteResult, error) {
					asked = cmd
					if throw {
						return RewriteResult{}, errors.New("rtk threw")
					}
					return RewriteResult{Code: entry.Code, Stdout: entry.Stdout}, nil
				}
			}
			if !hasRtk {
				// `engine: false` = the case ran with no rewrite at all; otherwise give a Rewrite that
				// must not be called (if it is, `asked` stops matching the golden's null).
				engine := true
				if raw, ok := c.Input["engine"]; ok {
					if err := json.Unmarshal(raw, &engine); err != nil {
						t.Fatal(err)
					}
				}
				if engine {
					ctx.Rewrite = func(cmd string) (RewriteResult, error) {
						asked = cmd
						return RewriteResult{Code: 1}, nil
					}
				}
			}
			d := Decide(c.str(t, "command"), ctx)
			dec := map[string]any{"action": d.Action, "reason": d.Reason}
			if d.Action == "rewrite" {
				dec["command"] = d.Command
				dec["allow"] = d.Allow
			}
			checkOutput(t, c, map[string]any{"decision": dec, "asked": asked})
		})
	}
}

func TestGoldenPermissions(t *testing.T) {
	for _, c := range loadGolden(t, "permissions") {
		t.Run(c.File+"/"+c.Name, func(t *testing.T) {
			cmd := c.str(t, "command")
			got := map[string]any{}
			if _, ok := c.Output["originalVerdict"]; ok {
				var rules struct {
					Allow []string `json:"allow"`
					Deny  []string `json:"deny"`
				}
				if err := json.Unmarshal(c.Input["rules"], &rules); err != nil {
					t.Fatal(err)
				}
				got["originalVerdict"] = OriginalVerdict(cmd, Rules{Allow: rules.Allow, Deny: rules.Deny})
			} else {
				got["commandSegments"] = CommandSegments(cmd)
			}
			checkOutput(t, c, got)
		})
	}
}
