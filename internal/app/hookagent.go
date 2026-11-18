package app

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"github.com/mananos/nxy.dev/v2/internal/docs"
	"github.com/mananos/nxy.dev/v2/internal/domain"
	"github.com/mananos/nxy.dev/v2/internal/flow"
	"github.com/mananos/nxy.dev/v2/internal/memory"
	"github.com/mananos/nxy.dev/v2/internal/plan"
	"github.com/mananos/nxy.dev/v2/internal/review"
)

const (
	rolePlanner       = "nxy:planner"
	roleImplementer   = "nxy:implementer"
	roleReviewer      = "nxy:reviewer"
	roleReviewerQuick = "nxy:reviewer-quick"
	roleRefuter       = "nxy:refuter"
	roleScout         = "nxy:scout"
	roleDocumenter    = "nxy:documenter"
	// roleMain is the engineer-mode main thread, which is not an agent.
	roleMain = "main"
)

// memRole is the memory role of a dispatch role for the global rules; the others (scout, delegate,
// refuter, documenter) get none.
func memRole(role string) string {
	switch role {
	case rolePlanner:
		return memory.RolePlanner
	case roleImplementer:
		return memory.RoleImplementer
	case roleReviewer, roleReviewerQuick:
		return memory.RoleReviewer
	case roleMain:
		return memory.RoleMain
	}
	return ""
}

// roleFor is the nxy subagent each dispatch action expects.
var roleFor = map[domain.ActionKind]string{
	domain.DispatchPlanner:     rolePlanner,
	domain.DispatchImplementer: roleImplementer,
	domain.DispatchAffectedFix: roleImplementer,
	domain.DispatchReviewer:    roleReviewer,
	domain.DispatchRefuter:     roleRefuter,
	domain.DispatchFixer:       roleImplementer,
	domain.DispatchDocumenter:  roleDocumenter,
}

// roleInfo is what nxy does with a subagent that can run in the background: the phase it works
// in and the events its return stands for (it may change the feature, e.g. the verdict).
type roleInfo map[domain.Phase]func(f *domain.Feature) []flow.Event

var roles = map[string]roleInfo{
	rolePlanner: {domain.PhasePlanning: func(f *domain.Feature) []flow.Event {
		// A plan held back by a busy repo is waiting for the user, not missing.
		return []flow.Event{{Kind: flow.PlannerReturned, Blocked: len(f.Clash) > 0 && f.PendingPlan != nil}}
	}},
	roleImplementer: {
		domain.PhaseAffectedFix: func(*domain.Feature) []flow.Event {
			return []flow.Event{{Kind: flow.FixesReturned}}
		},
		domain.PhaseFixing: func(*domain.Feature) []flow.Event {
			return []flow.Event{{Kind: flow.FixesReturned}}
		},
	},
	roleRefuter: {domain.PhaseRefuting: refuterReturned},
	roleDocumenter: {domain.PhaseDocumenting: func(*domain.Feature) []flow.Event {
		return []flow.Event{{Kind: flow.DocumenterReturned}}
	}},
}

// batchReturnedN judges batch n from the proof the binary recorded: BatchReturned, then Green
// or Red. The reason of a red stays in Proof.Failure for the retry prompt and the question.
func batchReturnedN(f *domain.Feature, n int) []flow.Event {
	p := f.ProofOf(n)
	ok, why := false, "the feature has no plan"
	if f.Plan != nil {
		pr := p
		ok, why = plan.Verdict(plan.NeedOf(*f.Plan, n), &pr, n)
	}
	if ok {
		p.Failure = ""
	} else {
		p.Failure = why
	}
	f.SetProof(p)
	kind := flow.Red
	if ok {
		kind = flow.Green
	}
	return []flow.Event{{Kind: flow.BatchReturned, Batch: n}, {Kind: kind, Batch: n}}
}

// batchOfPrompt is the batch number an implementer prompt names ("feature X batch 2"); 0 if none.
func batchOfPrompt(prompt string) int {
	m := batchPromptRe.FindStringSubmatch(prompt)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

var batchPromptRe = regexp.MustCompile(`(?i)\bbatch (\d+)`)

// noteWrite counts a subagent write in the repo during running batch n: a green recorded
// before it no longer vouches for the code.
func (h Hooks) noteWrite(ctx context.Context, id string, n int) {
	_, _, _ = h.mutate(ctx, id, func(g *domain.Feature) bool {
		if g.State.Phase != domain.PhaseRunningBatch {
			return false
		}
		p := g.ProofOf(n)
		p.Edits++
		g.SetProof(p)
		return true
	})
}

// backgroundEnded marks that a background-capable subagent's SubagentStop arrived before any
// background launch.
const backgroundEnded = "ended"

// backgroundWait is the line telling the main thread a background subagent is still running.
func backgroundWait(id, role string) string {
	return "nxy: feature " + id + " — the " + strings.TrimPrefix(role, "nxy:") + " is running in the background; wait for it and do not dispatch it again: nxy brings its result when it ends."
}

func isAgentTool(tool string) bool { return tool == "Agent" || tool == "Task" }

// activeFeature is the active feature of the event, if any.
func (h Hooks) activeFeature(ctx context.Context, ev HookEvent) (domain.Feature, bool) {
	f, found := h.featureFor(ctx, ev)
	if !found {
		return domain.Feature{}, false
	}
	return h.ensureRules(ctx, h.ensureDocs(ctx, h.ensureReview(ctx, f))), true
}

// featureFor finds the active feature an event belongs to, in this order: the one its session is
// linked to (a subagent runs with the session of the main thread); the one holding the checkout of
// its folder; the one holding the repo key (a feature from before the checkouts, adopted here); and
// the newest active one started from the folder (it released it when it went to worktrees).
func (h Hooks) featureFor(ctx context.Context, ev HookEvent) (domain.Feature, bool) {
	if h.Store == nil || h.Repos == nil {
		return domain.Feature{}, false
	}
	if f, ok := h.linkedFeature(ctx, ev); ok {
		return f, true
	}
	dir := ev.ProjectDir
	if dir == "" {
		dir = ev.Cwd
	}
	checkout := h.Repos.Checkout(dir)
	if f, found, err := h.Store.ActiveFeature(ctx, checkout); err == nil && found {
		return f, true
	}
	if f, found, err := h.Store.ActiveFeature(ctx, h.Repos.Key(dir)); err == nil && found {
		if f.Home == "" {
			f = h.adoptFeature(ctx, f, checkout)
		}
		return f, true
	}
	if fs, err := h.Store.Features(ctx, FeatureFilter{Home: checkout, Active: true}); err == nil && len(fs) > 0 {
		return fs[0], true
	}
	return domain.Feature{}, false
}

// adoptFeature gives a feature from before the checkouts its home and the hold of that checkout.
func (h Hooks) adoptFeature(ctx context.Context, f domain.Feature, checkout string) domain.Feature {
	g, ok, err := h.mutate(ctx, f.ID, func(g *domain.Feature) bool {
		if g.Home != "" || g.State.Phase == domain.PhaseDone {
			return false
		}
		g.Home, g.Holds = checkout, []string{checkout}
		return true
	})
	if err == nil && (ok || g.ID != "") {
		return g
	}
	return f
}

// preToolAgent handles PreToolUse Agent/Task from the main thread: it injects the planner's
// prompt and refuses nxy roles that are not the next step.
func (h Hooks) preToolAgent(ctx context.Context, ev HookEvent) HookResponse {
	resp := h.preToolAgentFlow(ctx, ev)
	if resp.Deny != "" || ev.AgentID != "" || !strings.HasPrefix(ev.Subagent, "nxy:") {
		return resp
	}
	if ev.Subagent == roleScout && resp.Prompt == "" {
		resp.Prompt = h.bootstrapPrompt(ctx, ev)
	}
	if ev.Subagent == roleScout && resp.Prompt == "" && h.Exe != "" {
		resp.Prompt = scoutPrompt(ev.AgentPrompt, h.Exe)
	}
	if h.Roles == nil {
		return resp
	}
	// The role table of the config fills what the dispatch does not say; the dispatch wins.
	dir := ev.ProjectDir
	if dir == "" {
		dir = ev.Cwd
	}
	model, effort := h.Roles.Role(dir, strings.TrimPrefix(ev.Subagent, "nxy:"))
	if ev.AgentModel == "" {
		resp.Model = model
	}
	if ev.AgentEffort == "" {
		resp.Effort = effort
	}
	return resp
}

// scoutQuestion is the dispatch prompt as one line the scout can pass to `locate`: characters a shell
// would read as syntax become spaces (so the command stays one plain call nxy can pre-approve) and
// it is cut at 200 characters, enough for the eight terms the search takes.
func scoutQuestion(prompt string) string {
	var b strings.Builder
	for _, r := range prompt {
		// the curly quotes U+2018..U+201E are quotes for PowerShell
		if strings.ContainsRune("\"'$`\\;&|<>()", r) || r < ' ' || (r >= '‘' && r <= '„') {
			r = ' '
		}
		b.WriteRune(r)
	}
	q := strings.Join(strings.Fields(b.String()), " ")
	if rs := []rune(q); len(rs) > 200 {
		q = strings.TrimSpace(string(rs[:200]))
	}
	return q
}

// scoutPrompt is the dispatch prompt of the scout plus the exact command to run before it reads
// anything: the model-free search, whose dense output dies with the scout.
func scoutPrompt(prompt, exe string) string {
	exe = strings.ReplaceAll(exe, `\`, "/")
	q := scoutQuestion(prompt)
	if q == "" {
		q = "<question>"
	}
	return strings.TrimRight(prompt, " \t\r\n") + "\n\n" +
		"nxy: before you read any file or use Grep, run this and read only what it points at (in PowerShell, start it with `& `):\n" +
		"\"" + exe + "\" locate \"" + q + "\"\n" +
		"For who-calls, what-it-calls or what-breaks questions use `\"" + exe + "\" locate --callers <symbol>` (or `--callees`, `--impact`). It never changes the repo."
}

// preToolAgentFlow is the part of preToolAgent that belongs to the feature flow.
func (h Hooks) preToolAgentFlow(ctx context.Context, ev HookEvent) HookResponse {
	if ev.AgentID != "" || !strings.HasPrefix(ev.Subagent, "nxy:") || ev.Subagent == roleScout {
		return HookResponse{}
	}
	f, ok := h.activeFeature(ctx, ev)
	if !ok {
		return HookResponse{}
	}
	if isReviewerRole(ev.Subagent) {
		return h.preReviewer(ctx, ev, f)
	}
	if ev.Subagent == roleImplementer && f.State.Phase == domain.PhaseRunningBatch {
		return h.preImplementer(ctx, ev, f)
	}
	switch {
	case f.Background == ev.Subagent:
		return HookResponse{Deny: backgroundWait(f.ID, ev.Subagent)}
	case ev.Subagent == rolePlanner && f.State.Phase == domain.PhasePlanning:
		return HookResponse{Prompt: plan.PlannerPrompt(f, h.Exe, h.memoryFor(ctx, f, rolePlanner, nil).Text)}
	case ev.Subagent == roleRefuter && f.State.Phase == domain.PhaseRefuting:
		return HookResponse{Prompt: review.RefuterPrompt(f, h.Exe)}
	case ev.Subagent == roleDocumenter && f.State.Phase == domain.PhaseDocumenting:
		return HookResponse{Prompt: docs.DocumenterPrompt(h.docsAbsolute(f), h.Exe)}
	case ev.Subagent == roleImplementer && f.State.Phase == domain.PhaseFixing:
		return HookResponse{Prompt: review.FixPrompt(f, h.Exe, h.implementerMemory(ctx, f, findingFiles(f)))}
	case ev.Subagent == roleImplementer && f.State.Phase != domain.PhaseAffectedFix:
		return HookResponse{Deny: "plan not approved; next step " + f.Next.Label()}
	case ev.Subagent == roleImplementer && f.Next.Kind == domain.DispatchAffectedFix:
		return HookResponse{Prompt: plan.AffectedFixPrompt(f, h.Exe, h.implementerMemory(ctx, f, h.changedPaths(ctx, f)))}
	case ev.Subagent != roleFor[f.Next.Kind]:
		return HookResponse{Deny: "out of order: next step is " + f.Next.Label()}
	}
	return HookResponse{}
}

// postToolAgent handles PostToolUse Agent/Task for the planner and the implementer: a
// background launch waits for SubagentStop; otherwise the return is judged (the planner
// without a saved plan retries or gives up, a batch is verified) and the stored step repeated.
func (h Hooks) postToolAgent(ctx context.Context, ev HookEvent) HookResponse {
	if isReviewerRole(ev.Subagent) {
		return h.postReviewer(ctx, ev)
	}
	role, known := roles[ev.Subagent]
	if ev.AgentID != "" || !known {
		return HookResponse{}
	}
	f, ok := h.activeFeature(ctx, ev)
	if !ok {
		return HookResponse{}
	}
	if ev.Subagent == roleImplementer && f.State.Phase == domain.PhaseRunningBatch {
		return h.postImplementer(ctx, ev, f)
	}
	ended := f.Background == backgroundEnded
	returned, inPhase := role[f.State.Phase]
	if ev.AgentAsync && !ended && inPhase {
		nf, _, err := h.mutate(ctx, f.ID, func(g *domain.Feature) bool {
			g.Background = ev.Subagent
			return true
		})
		if err != nil {
			return HookResponse{}
		}
		return HookResponse{Context: backgroundWait(nf.ID, ev.Subagent)}
	}
	if inPhase {
		nf, _, err := h.advanceSteps(ctx, f.ID, func(g *domain.Feature) ([]flow.Event, error) {
			if g.Background == backgroundEnded {
				g.Background = ""
			}
			return h.withCandidates(g, returned(g)), nil
		})
		if err != nil {
			return HookResponse{}
		}
		f = nf
	} else if ended {
		if nf, _, err := h.mutate(ctx, f.ID, func(g *domain.Feature) bool {
			if g.Background != backgroundEnded {
				return false
			}
			g.Background = ""
			return true
		}); err == nil {
			f = nf
		}
	}
	return HookResponse{Next: &Next{Feature: f}}
}
