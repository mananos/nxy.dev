package views

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/mananos/nxy.dev/v2/internal/ask"
	"github.com/mananos/nxy.dev/v2/internal/domain"
	"github.com/mananos/nxy.dev/v2/internal/plan"
	"github.com/mananos/nxy.dev/v2/internal/repos"
)

// FileImpact is what the provider found out about one file of the plan: whether it exists at the
// base, who imports it (base names) and its tests.
type FileImpact struct {
	Exists     bool
	ImportedBy []string
	Tests      []TestRef
}

// FileKey is the key of PlanIn.Files: repo and path joined with a NUL.
func FileKey(repo, path string) string { return repo + "\x00" + path }

// PlanIn is what the plan screen needs. Files is keyed by FileKey(repo, path); Source is
// codegraph|name|none, as worked out by the provider.
type PlanIn struct {
	FeatureIn
	Files  map[string]FileImpact
	Source string
	// Waiting are the questions the hook holds in the TUI; the approval one gives approve and change their argv.
	Waiting []domain.PendingAsk
}

var atLineRe = regexp.MustCompile(`:(\d+)(?:-\d+)?$`)

// ParseAt reads a change location ("api/src/X.java:12", "src/X.java:3-9" or "src/X.java (new)"):
// the repo (when names tell it apart), the path inside it, the first line and whether it is a new file.
func ParseAt(at string, names []string) (repo, path string, line int, isNew bool) {
	s := strings.TrimSpace(at)
	if t, ok := strings.CutSuffix(s, " (new)"); ok {
		s, isNew = strings.TrimSpace(t), true
	} else if m := atLineRe.FindStringSubmatchIndex(s); m != nil {
		line, _ = strconv.Atoi(s[m[2]:m[3]])
		s = s[:m[0]]
	}
	s = strings.ReplaceAll(s, "\\", "/")
	if name, rel, ok := repos.Split(s, names); ok {
		repo, s = name, rel
	}
	return repo, strings.TrimPrefix(s, "./"), line, isNew
}

// RepoNames are the names that start a path in a multi-repo plan; none for a one-repo feature.
func RepoNames(f domain.Feature) []string {
	var out []string
	for _, r := range f.Repos {
		if r.Name != "" {
			out = append(out, r.Name)
		}
	}
	return out
}

// RepoOfChange is the repo name of a change: the one in its path, the batch's, or the first.
func RepoOfChange(f domain.Feature, pb domain.Batch, repo string) string {
	if repo != "" {
		return repo
	}
	if pb.Repo != "" {
		return pb.Repo
	}
	if rs := reposOf(f); len(rs) > 0 {
		return rs[0].Name
	}
	return ""
}

// BuildPlan builds the plan screen; false when the feature has no plan yet.
func BuildPlan(in PlanIn) (Plan, bool) {
	f := in.Feature
	if f.Plan == nil {
		return Plan{}, false
	}
	p := f.Plan
	h := Header(in.FeatureIn)
	out := Plan{
		Feature: h, State: PlanRunning, Goal: p.Title, Repos: reposOf(f),
		Criteria: []Criterion{}, Decisions: []PlanDecision{}, Batches: []PlanBatch{},
		Risks: append([]string{}, p.Risks...), Impact: Impact{Source: in.Source, Files: []ImpactFile{}},
		Actions: []Action{},
	}
	switch {
	case f.State.Phase == domain.PhaseDone:
		out.State = PlanDone
	case h.Stage == StagePlan || h.Stage == StageDesign:
		out.State = PlanApproval
	}
	if out.State != PlanApproval {
		var first int64
		for _, st := range f.State.Steps {
			if st.StartedAt > 0 && (first == 0 || st.StartedAt < first) {
				first = st.StartedAt
			}
		}
		out.ApprovedAt = rfc(first)
	}
	if out.State != PlanDone {
		out.Actions = append(out.Actions, lifeAction(f))
	}
	if out.State == PlanApproval {
		var row *domain.PendingAsk
		if a, ok := ask.For(f); ok {
			row = waitingRow(f, in.Waiting, asked(a.Questions))
		}
		out.Actions = append(out.Actions,
			Action{ID: "approve", Label: "Aprobar", Argv: decideArgv(row, plan.Approve)},
			Action{ID: "change", Label: "Cambiar", Argv: decideArgv(row, plan.Change)},
			Action{ID: "stop", Label: "Parar", Argv: []string{"nxy", "feature", "stop", "--feature", f.ID}, Confirm: "Termina la feature y suelta la pregunta"},
		)
	}
	if out.Impact.Source == "" {
		out.Impact.Source = "none"
	}

	for _, c := range p.Criteria {
		out.Criteria = append(out.Criteria, Criterion{ID: c.ID, Text: c.Text, Test: string(c.Test)})
	}
	fromDesign := map[string]bool{}
	if f.Design != nil {
		for _, d := range f.Design.Decisions {
			if d.ID != "" {
				fromDesign[d.ID] = true
			}
		}
	}
	for _, d := range p.Decisions {
		out.Decisions = append(out.Decisions, PlanDecision{
			Scope: string(d.Scope), Repo: d.Repo, Text: d.Text, Question: d.Question,
			FromDesign: d.Fork != "" || (d.ID != "" && fromDesign[d.ID]),
		})
	}

	steps := map[int]domain.Step{}
	for _, st := range f.State.Steps {
		steps[st.N] = st
	}
	names := RepoNames(f)
	for _, pb := range p.Batches {
		st, ok := steps[pb.N]
		b := PlanBatch{
			N: pb.N, Title: pb.Title, Repo: pb.Repo, Status: stepStatus(st.Status), Depends: nonNilInts(pb.Depends),
			Risk: string(pb.Risk), RiskWhy: pb.RiskWhy, Attempts: MaxAttempts, Accept: pb.Test.Accept,
			Changes: []Change{},
		}
		if b.Repo == "" && ok {
			b.Repo = st.Repo
		}
		if ok && (st.Launched || (st.Status != domain.StepPending && st.Status != domain.StepRunning && st.Status != "")) {
			b.Attempt = st.Attempt + 1
		}
		for _, c := range pb.Change {
			b.Changes = append(b.Changes, Change{At: c.At, What: c.What})
		}
		out.Batches = append(out.Batches, b)
		out.Impact.Files = appendImpact(out.Impact.Files, f, pb, names, in.Files)
	}
	return out, true
}

// appendImpact adds one entry per file of a batch, in plan order, with every line the batch changes in it.
func appendImpact(files []ImpactFile, f domain.Feature, pb domain.Batch, names []string, info map[string]FileImpact) []ImpactFile {
	idx := map[string]int{}
	for _, c := range pb.Change {
		r, path, line, isNew := ParseAt(c.At, names)
		r = RepoOfChange(f, pb, r)
		if path == "" {
			continue
		}
		i, seen := idx[FileKey(r, path)]
		if !seen {
			fi, known := info[FileKey(r, path)]
			kind := "edit"
			if isNew || (known && !fi.Exists) {
				kind = "new"
			}
			files = append(files, ImpactFile{
				Path: path, Repo: r, Batch: pb.N, Kind: kind, Lines: []FileChange{},
				ImportedBy: append([]string{}, fi.ImportedBy...), Tests: append([]TestRef{}, fi.Tests...),
			})
			i = len(files) - 1
			idx[FileKey(r, path)] = i
		} else if isNew {
			files[i].Kind = "new"
		}
		if line > 0 {
			files[i].Lines = append(files[i].Lines, FileChange{Line: line, What: c.What})
		}
	}
	return files
}
