package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/setup"
	"github.com/mananos/nxy.dev/internal/views"
)

// isolateView points every home the command reads at empty temp folders.
func isolateView(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("NXY_HOME", t.TempDir())
	proj := t.TempDir()
	t.Setenv("CLAUDE_PROJECT_DIR", proj)
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	return proj
}

func TestViewArgs(t *testing.T) {
	isolateView(t)
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"no id", []string{"view"}, "usage: nxy view"},
		{"missing --json", []string{"view", "cockpit"}, "usage: nxy view"},
		{"unknown id", []string{"view", "nope", "--json"}, "usage: nxy view"},
		{"bad scope", []string{"view", "memory", "--json", "--scope", "x"}, "usage: nxy view"},
		{"plan later", []string{"view", "plan", "--json"}, "llega en F5.2c"},
		{"file later", []string{"view", "file", "--json"}, "llega en F5.2c"},
		{"engineer later", []string{"view", "engineer", "--json"}, "llega en F5.2c"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var out, errb bytes.Buffer
			if code := run(c.args, strings.NewReader(""), &out, &errb); code != 1 {
				t.Fatalf("exit %d, want 1", code)
			}
			if out.Len() != 0 || !strings.Contains(errb.String(), c.want) {
				t.Fatalf("stdout %q stderr %q, want %q on stderr only", out.String(), errb.String(), c.want)
			}
		})
	}
}

func TestViewEmptyDB(t *testing.T) {
	isolateView(t)
	var out, errb bytes.Buffer
	if code := run([]string{"view", "cockpit", "--json"}, strings.NewReader(""), &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	var c views.Cockpit
	if err := views.Decode(out.Bytes(), &c); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if len(c.Features) != 0 || len(c.Worktrees) != 0 || c.Totals.Active != 0 || c.Totals.NeedsYou != 0 {
		t.Fatalf("empty db should give an empty cockpit: %+v", c)
	}
	if !strings.Contains(out.String(), `"features": []`) {
		t.Fatalf("features must be [] not null:\n%s", out.String())
	}
}

func TestViewEmptyDBOthers(t *testing.T) {
	isolateView(t)
	oldS, oldB := viewSetupState, viewBinary
	viewSetupState = func(string, func(string) string) setup.State { return setup.State{} }
	viewBinary = func() views.Binary { return views.Binary{Installed: "x", Wanted: "x", Download: "none"} }
	t.Cleanup(func() { viewSetupState, viewBinary = oldS, oldB })
	for _, args := range [][]string{
		{"memory", "--json"}, {"memory", "--json", "--scope", "global"}, {"trend", "--json"}, {"config", "--json"},
	} {
		var out, errb bytes.Buffer
		if code := run(append([]string{"view"}, args...), strings.NewReader(""), &out, &errb); code != 0 {
			t.Fatalf("%v: exit %d: %s", args, code, errb.String())
		}
		v := views.New(args[0])
		if err := views.Decode(out.Bytes(), v); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out.String())
		}
	}
	var out, errb bytes.Buffer
	if code := run([]string{"view", "stats", "--json"}, strings.NewReader(""), &out, &errb); code != 1 || !strings.Contains(errb.String(), "nxy view stats:") {
		t.Fatalf("stats with no session: exit %d stderr %q", code, errb.String())
	}
}

func TestViewReceivedCitedPerAgent(t *testing.T) {
	cat := memCatalog{byRepo: map[string]map[string]views.MemRef{
		"a": {"K1": {ID: "K1", Kind: "rule", Text: "de a"}},
	}}
	prompt := "Reglas: K1"
	one := cat.received([]string{"a"}, prompt, "Respeté K1 en el cambio")
	two := cat.received([]string{"a"}, prompt, "listo")
	if len(one) != 1 || !one[0].Cited {
		t.Fatalf("the agent that named K1 should cite it: %+v", one)
	}
	if len(two) != 1 || two[0].Cited {
		t.Fatalf("the other agent received K1 and did not cite it: %+v", two)
	}
}

func TestViewReceivedResolvesByRepo(t *testing.T) {
	cat := memCatalog{
		byRepo: map[string]map[string]views.MemRef{
			"a": {"K1": {ID: "K1", Kind: "rule", Text: "de a"}},
			"b": {"K1": {ID: "K1", Kind: "rule", Text: "de b"}},
		},
		global: map[string]views.MemRef{"G1": {ID: "G1", Kind: "rule", Text: "global"}},
	}
	f := domain.Feature{
		Repos: []domain.FeatureRepo{{Name: "api", Key: "a"}, {Name: "web", Key: "b"}},
		State: domain.State{Steps: []domain.Step{{N: 1, Repo: "api"}, {N: 2, Repo: "web"}}},
	}
	for batch, want := range map[int]string{0: "de a", 1: "de a", 2: "de b"} {
		got := cat.received(repoOrder(f, batch), "K1 G1", "")
		if len(got) != 2 || got[0].Text != want || got[1].Text != "global" {
			t.Fatalf("batch %d: %+v, want K1 = %q", batch, got, want)
		}
	}
}

func TestViewDecisionWithoutFeature(t *testing.T) {
	isolateView(t)
	var out, errb bytes.Buffer
	if code := run([]string{"view", "decision", "--json"}, strings.NewReader(""), &out, &errb); code != 1 {
		t.Fatalf("exit %d, want 1 (no active feature)", code)
	}
	if !strings.Contains(errb.String(), "no hay una feature activa") {
		t.Fatalf("stderr %q", errb.String())
	}
}
