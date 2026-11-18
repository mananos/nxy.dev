package worktree

import (
	"testing"

	"github.com/mananos/nxy.dev/v2/internal/goldentest"
)

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func TestGolden(t *testing.T) {
	for _, c := range goldentest.Load(t, "worktree") {
		t.Run(c.File+"/"+c.Name, func(t *testing.T) {
			switch c.File {
			case "parse.json":
				var porcelain string
				c.In(t, "porcelain", &porcelain)
				list := []any{}
				for _, w := range ParseWorktrees(porcelain) {
					list = append(list, map[string]any{
						"path": w.Path, "branch": nullable(w.Branch), "head": w.Head,
						"detached": w.Detached, "locked": w.Locked, "main": w.Main,
					})
				}
				goldentest.Check(t, c, map[string]any{"worktrees": list})
			case "slug.json":
				var text string
				c.In(t, "text", &text)
				goldentest.Check(t, c, map[string]any{"slug": SlugOf(text)})
			case "issue.json":
				var text string
				c.In(t, "text", &text)
				r := ParseIssue(text)
				goldentest.Check(t, c, map[string]any{"ok": r.OK, "issue": r.Issue, "error": r.Error})
			case "names.json":
				var in struct {
					Issue  string  `json:"issue"`
					Name   *string `json:"name"`
					Base   string  `json:"base"`
					Branch *string `json:"branch"`
				}
				var main string
				c.In(t, "input", &in)
				c.In(t, "mainPath", &main)
				n := FeatureNamesOf(NamesInput{Issue: in.Issue, Name: in.Name, Base: in.Base, Branch: in.Branch}, main)
				goldentest.Check(t, c, map[string]any{
					"ok": n.OK, "create": n.Create, "slug": n.Slug, "branch": n.Branch,
					"base": nullable(n.Base), "path": n.Path, "error": n.Error,
				})
			case "default.json":
				var in struct {
					OriginHead string   `json:"originHead"`
					MainBranch string   `json:"mainBranch"`
					Branches   []string `json:"branches"`
				}
				c.In(t, "originHead", &in.OriginHead)
				c.In(t, "mainBranch", &in.MainBranch)
				c.In(t, "branches", &in.Branches)
				goldentest.Check(t, c, map[string]any{"branch": nullable(DefaultBranchOf(in.OriginHead, in.MainBranch, in.Branches))})
			case "bases.json":
				var branches []string
				var def string
				var wts []struct {
					Path   string `json:"path"`
					Branch string `json:"branch"`
				}
				c.In(t, "branches", &branches)
				c.In(t, "default", &def)
				c.In(t, "worktrees", &wts)
				var list []Worktree
				for _, w := range wts {
					list = append(list, Worktree{Path: w.Path, Branch: w.Branch})
				}
				goldentest.Check(t, c, map[string]any{"bases": BaseChoices(branches, def), "choices": BranchChoices(branches, list)})
			case "add.json":
				var branch, path, base string
				var create bool
				c.In(t, "branch", &branch)
				c.In(t, "path", &path)
				c.In(t, "create", &create)
				c.In(t, "base", &base)
				argv, err := AddArgv(branch, path, create, base)
				if err != nil {
					goldentest.Check(t, c, map[string]any{"error": err.Error()})
					return
				}
				goldentest.Check(t, c, map[string]any{"argv": argv})
			case "remove.json":
				var path string
				var force bool
				c.In(t, "path", &path)
				c.In(t, "force", &force)
				goldentest.Check(t, c, map[string]any{"argv": RemoveArgv(path, force)})
			case "command.json":
				var argv []string
				var shell string
				c.In(t, "argv", &argv)
				c.In(t, "shell", &shell)
				goldentest.Check(t, c, map[string]any{"text": CommandText(argv, Shell(shell))})
			case "shell.json":
				var platform string
				var env struct {
					Shell string `json:"SHELL"`
				}
				c.In(t, "platform", &platform)
				c.In(t, "env", &env)
				if platform == "win32" {
					platform = "windows" // Node's name for GOOS
				}
				goldentest.Check(t, c, map[string]any{"shell": string(ShellOf(platform, env.Shell))})
			case "open.json":
				var path, shell string
				c.In(t, "path", &path)
				c.In(t, "shell", &shell)
				goldentest.Check(t, c, map[string]any{"command": OpenCommand(path, Shell(shell))})
			case "row.json":
				var in struct {
					Now int64 `json:"now"`
					Wt  struct {
						Path        string  `json:"path"`
						Branch      *string `json:"branch"`
						Main        bool    `json:"main"`
						Current     bool    `json:"current"`
						Dirty       *int    `json:"dirty"`
						Ahead       int     `json:"ahead"`
						BatchDone   int     `json:"batchDone"`
						BatchTotal  int     `json:"batchTotal"`
						ReviewDone  bool    `json:"reviewDone"`
						HandoffAt   int64   `json:"handoffAt"`
						PausedAt    int64   `json:"pausedAt"`
						Running     bool    `json:"running"`
						MissingDeps bool    `json:"missingDeps"`
					} `json:"wt"`
				}
				c.In(t, "now", &in.Now)
				c.In(t, "wt", &in.Wt)
				w := in.Wt
				ri := RowInput{
					Path: w.Path, Main: w.Main, Current: w.Current, Dirty: w.Dirty, Ahead: w.Ahead, BatchDone: w.BatchDone,
					BatchTotal: w.BatchTotal, ReviewDone: w.ReviewDone, HandoffAt: w.HandoffAt, PausedAt: w.PausedAt,
					Running: w.Running, MissingDeps: w.MissingDeps,
				}
				if w.Branch != nil {
					ri.Branch = *w.Branch
				}
				r := RowView(ri, in.Now)
				goldentest.Check(t, c, map[string]any{"name": r.Name, "branch": r.Branch, "tags": r.Tags, "lines": r.Lines})
			default:
				t.Fatalf("unknown golden file %s", c.File)
			}
		})
	}
}
