package setup

import (
	"encoding/json"
	"strings"
	"testing"
)

func handState() State {
	rtkPath := "/home/dev/.local/bin/rtk"
	return State{
		Platform: "linux",
		Rtk: ToolInfo{Found: true, Path: rtkPath, Version: "0.5.1", Conflict: ConflictYes,
			Plan: InstallPlan("rtk", Opts{Platform: "linux", Found: true, Conflict: ConflictYes, RtkPath: &rtkPath})},
		Rg:        ToolInfo{Found: true, Path: "rg", OnPath: true, Plan: InstallPlan("rg", Opts{Platform: "linux", PM: "apt-get", Found: true})},
		Codegraph: ToolInfo{Plan: InstallPlan("codegraph", Opts{Platform: "linux"})},
	}
}

func TestStatusText(t *testing.T) {
	text := StatusText(handState())
	for _, want := range []string{
		"nxy setup (linux)\n",
		"\nrtk: encontrada 0.5.1 (/home/dev/.local/bin/rtk) — sólo en un directorio conocido (una terminal nueva la ve en el PATH)\n",
		"  choca: hook propio de rtk en tus settings\n",
		"  quita:   /home/dev/.local/bin/rtk init -g --uninstall   (rtk init --help)\n",
		"\nrg: encontrada (rg) — en el PATH\n",
		"  a mano:  sudo apt-get install -y ripgrep\n",
		"\ncodegraph: no encontrada — no encontrada\n",
		"  install: t=$(mktemp)",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("status lacks %q:\n%s", want, text)
		}
	}
	if !strings.HasSuffix(text, "\nNada se instaló. Para instalar: nxy setup run <herramienta> --yes") {
		t.Errorf("status ends wrongly:\n%s", text)
	}
}

func TestStatusJSONShape(t *testing.T) {
	line := StatusJSON(handState())
	if strings.Contains(line, "\n") || strings.Contains(line, `\u0026`) {
		t.Fatalf("not one raw line: %s", line)
	}
	var got struct {
		OK       bool
		Platform string
		Tools    map[string]map[string]any
	}
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatal(err)
	}
	if !got.OK || got.Platform != "linux" || len(got.Tools) != 3 {
		t.Fatalf("got %+v", got)
	}
	if got.Tools["rtk"]["conflict"] != true || got.Tools["rg"]["conflict"] != false {
		t.Errorf("conflict: %v / %v", got.Tools["rtk"]["conflict"], got.Tools["rg"]["conflict"])
	}
	if got.Tools["codegraph"]["path"] != nil || got.Tools["rg"]["version"] != nil {
		t.Errorf("unknown path/version must be null: %v", got.Tools)
	}
	if !strings.HasPrefix(line, `{"ok":true,"platform":"linux","tools":{"rtk":{"found":true,"path":`) {
		t.Errorf("key order changed: %.90s", line)
	}
}

func TestRunOutcomeAndHeader(t *testing.T) {
	p := InstallPlan("rtk", Opts{Platform: "win32"})
	if h := RunHeader("rtk", p, true); !strings.HasPrefix(h, "install rtk (dry-run: no se ejecuta nada):\n  install: winget install rtk-ai.rtk") {
		t.Errorf("header: %q", h)
	}
	text, code := RunOutcome("rtk", p, RunResult{OK: true, Ran: []string{"x"}, Tail: "done"})
	if code != 0 || text != "done\nrtk: listo (1 paso)." {
		t.Errorf("ok outcome %d %q", code, text)
	}
	text, code = RunOutcome("rg", p, RunResult{OK: false, Ran: []string{"a"}, FailedAt: "a", Tail: "boom", Manual: []string{"sudo x", "y"}})
	if code != 1 || text != "boom\nFalló en: a\nPara correrlo en una terminal: sudo x ; y" {
		t.Errorf("failed outcome %d %q", code, text)
	}
	// plain `sudo` fallbacks belong to steps that ran fine; other notes stay
	pl := Plan{Manual: []string{"sudo apt-get install -y ripgrep", "quitalo a mano"}}
	text, _ = RunOutcome("rg", pl, RunResult{OK: true, Ran: []string{"a", "b"}})
	if text != "rg: listo (2 pasos).\na mano: quitalo a mano" {
		t.Errorf("manual notes: %q", text)
	}
}
