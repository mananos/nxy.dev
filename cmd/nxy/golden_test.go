package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/matyy-a/nxy.dev/internal/filter"
	"github.com/matyy-a/nxy.dev/internal/host/claudecode"
)

type goldenHookCase struct {
	Name  string `json:"name"`
	Input struct {
		Payload       json.RawMessage   `json:"payload"`
		Env           map[string]string `json:"env"`
		UserConfig    any               `json:"userConfig"`
		ProjectConfig any               `json:"projectConfig"`
		Settings      struct {
			User    any `json:"user"`
			Project any `json:"project"`
			Local   any `json:"local"`
		} `json:"settings"`
		Engine *struct {
			Engine          string `json:"engine"`
			RtkPath         string `json:"rtkPath"`
			RtkHookDetected bool   `json:"rtkHookDetected"`
		} `json:"engine"`
		Rtk map[string]struct {
			Code   int    `json:"code"`
			Stdout string `json:"stdout"`
		} `json:"rtk"`
	} `json:"input"`
	Output struct {
		Stdout string `json:"stdout"`
	} `json:"output"`
}

// replayEngine answers from the case: the engine info it declares and the recorded rtk answers.
type replayEngine struct{ c *goldenHookCase }

func (r replayEngine) Info(filter.Settings, string) filter.EngineInfo {
	e := r.c.Input.Engine
	if e == nil {
		return filter.EngineInfo{Engine: "rtk", RtkPath: "rtk"}
	}
	info := filter.EngineInfo{Engine: e.Engine, RtkHookDetected: e.RtkHookDetected}
	if e.Engine == "rtk" {
		info.RtkPath = e.RtkPath
		if e.RtkPath == "abs" {
			info.RtkPath = "/golden/rtk"
		}
	}
	return info
}

func (r replayEngine) Rewrite(_ filter.EngineInfo, cmd string) (filter.RewriteResult, error) {
	if a, ok := r.c.Input.Rtk[cmd]; ok {
		return filter.RewriteResult{Code: a.Code, Stdout: a.Stdout}, nil
	}
	return filter.RewriteResult{Code: 1}, nil
}

func writeGoldenJSON(t *testing.T, path string, v any) {
	t.Helper()
	if v == nil {
		return
	}
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestGoldenHookPreToolUseBash(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join("..", "..", "testdata", "golden", "hook-pretooluse-bash", "*.json"))
	if len(files) == 0 {
		t.Skip("no hook golden files")
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var cases []goldenHookCase
		if err := json.Unmarshal(data, &cases); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		for i := range cases {
			c := &cases[i]
			t.Run(c.Name, func(t *testing.T) {
				root := t.TempDir()
				home := filepath.Join(root, "home")
				nxyhome := filepath.Join(root, "nxyhome")
				project := filepath.Join(root, "project")
				writeGoldenJSON(t, filepath.Join(home, ".claude", "settings.json"), c.Input.Settings.User)
				writeGoldenJSON(t, filepath.Join(project, ".claude", "settings.json"), c.Input.Settings.Project)
				writeGoldenJSON(t, filepath.Join(project, ".claude", "settings.local.json"), c.Input.Settings.Local)
				writeGoldenJSON(t, filepath.Join(nxyhome, "config.json"), c.Input.UserConfig)
				writeGoldenJSON(t, filepath.Join(project, ".nxy", "config.json"), c.Input.ProjectConfig)
				t.Setenv("HOME", home)
				t.Setenv("USERPROFILE", home)
				t.Setenv("NXY_HOME", nxyhome)
				t.Setenv("CLAUDE_PROJECT_DIR", project)
				t.Setenv("NXY_FILTER", "")
				t.Setenv("NXY_ENGINE", "")
				for k, v := range c.Input.Env {
					t.Setenv(k, v)
				}

				ev, err := claudecode.Decode(bytes.NewReader(c.Input.Payload))
				if err != nil {
					t.Fatal(err)
				}
				if ev.Name == "" {
					ev.Name = "PreToolUse" // the 1.x hook is registered for PreToolUse; the corpus payloads omit the name
				}
				h := newHooks()
				h.Engine = replayEngine{c}
				resp := h.Handle(context.Background(), ev)
				var out bytes.Buffer
				if err := claudecode.Encode(&out, ev, resp); err != nil {
					t.Fatal(err)
				}
				want, got := c.Output.Stdout, out.String()
				if want == "" || got == "" {
					if want != got {
						t.Fatalf("want %q, got %q", want, got)
					}
					return
				}
				var w, g any
				if err := json.Unmarshal([]byte(want), &w); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal([]byte(got), &g); err != nil {
					t.Fatalf("got invalid JSON %q: %v", got, err)
				}
				if !reflect.DeepEqual(w, g) {
					t.Fatalf("payload %s\nwant %s\ngot  %s", c.Input.Payload, want, got)
				}
			})
		}
	}
}
