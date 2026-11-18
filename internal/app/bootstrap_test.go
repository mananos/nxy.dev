package app

import (
	"context"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/v2/internal/memory"
)

type bootMemory struct {
	deliverMemory
	added []memory.Rule
}

func (m *bootMemory) AddRule(_ context.Context, r memory.Rule) (memory.Rule, error) {
	m.added = append(m.added, r)
	r.Short = "K" + string(rune('0'+len(m.added)))
	return r, nil
}

type fakeBootstrap struct {
	rows map[string]BootstrapState
}

func (f *fakeBootstrap) Bootstrap(_ context.Context, repo string) (BootstrapState, bool, error) {
	b, ok := f.rows[repo]
	return b, ok, nil
}
func (f *fakeBootstrap) SaveBootstrap(_ context.Context, b BootstrapState) error {
	f.rows[b.Repo] = b
	return nil
}
func (f *fakeBootstrap) MarkOffered(_ context.Context, repo string) (bool, error) {
	b := f.rows[repo]
	if b.OfferedAt != 0 {
		return false, nil
	}
	b.Repo, b.OfferedAt = repo, 1
	f.rows[repo] = b
	return true, nil
}

func bootCands(n int) []memory.BootstrapCandidate {
	var out []memory.BootstrapCandidate
	for i := 0; i < n; i++ {
		out = append(out, memory.BootstrapCandidate{
			Text: "regla " + string(rune('a'+i)), Question: "pregunta " + string(rune('a'+i)),
			Evidence: []string{"Makefile:1"}, Paths: []string{"src/**"},
		})
	}
	return out
}

func bootHooks(t *testing.T) (Hooks, *bootMemory, *fakeBootstrap) {
	t.Helper()
	h, _ := flowHooks(false)
	h.Exe = "/bin/nxy"
	mem := &bootMemory{}
	bs := &fakeBootstrap{rows: map[string]BootstrapState{}}
	h.Memory, h.Bootstrap = mem, bs
	return h, mem, bs
}

func bootAnswerEv(answers map[string]string) HookEvent {
	return HookEvent{Name: "PostToolUse", Tool: toolAskUser, ProjectDir: "/p", Cwd: "/p", Answers: answers}
}

func scoutDone() HookEvent {
	return HookEvent{Name: "PostToolUse", Tool: "Agent", Subagent: roleScout, AgentPrompt: BootstrapPromptText, ProjectDir: "/p", Cwd: "/p"}
}

func TestBootstrapOfferedOnceAndNoCreatesTheFeature(t *testing.T) {
	h, _, bs := bootHooks(t)
	ctx := context.Background()
	r := h.Handle(ctx, prompt("/nxy:feature add a flag"))
	if r.Ask == nil || r.Ask.Question != bootstrapOfferQuestion || r.Next != nil {
		t.Fatalf("first feature = %+v", r)
	}
	if st := bs.rows["repo:/p"]; st.PendingRequest != "add a flag" {
		t.Errorf("pending = %+v", st)
	}
	r = h.Handle(ctx, bootAnswerEv(map[string]string{bootstrapOfferQuestion: "No"}))
	if r.Next == nil || r.Next.Feature.Request != "add a flag" {
		t.Fatalf("after No = %+v", r)
	}
	if st := bs.rows["repo:/p"]; st.PendingRequest != "" {
		t.Errorf("pending kept: %+v", st)
	}
	// a clean store stands for the first feature having ended
	h.Store = newMemStore()
	r = h.Handle(ctx, prompt("/nxy:feature another"))
	if r.Ask != nil || r.Next == nil {
		t.Fatalf("second feature offered again: %+v", r)
	}
}

func TestBootstrapNotOfferedWithRulesOrWithoutPorts(t *testing.T) {
	h, mem, _ := bootHooks(t)
	mem.rules = []memory.Rule{{Text: "x", Question: "q"}}
	if r := h.Handle(context.Background(), prompt("/nxy:feature a")); r.Ask != nil || r.Next == nil {
		t.Fatalf("with rules = %+v", r)
	}
	h2, _ := flowHooks(false)
	if r := h2.Handle(context.Background(), prompt("/nxy:feature a")); r.Ask != nil || r.Next == nil {
		t.Fatalf("without ports = %+v", r)
	}
}

func scoutPre() HookEvent {
	return HookEvent{Name: "PreToolUse", Tool: "Agent", Subagent: roleScout, AgentPrompt: BootstrapPromptText, ProjectDir: "/p", Cwd: "/p"}
}

func scoutSave(exe string) HookEvent {
	return HookEvent{Name: "PreToolUse", Tool: "Bash", AgentID: "a1", AgentType: roleScout, ProjectDir: "/p", Cwd: "/p",
		Command: "\"" + exe + "\" mem bootstrap save <<'JSON'\n[]\nJSON\n", HasCommand: true}
}

func TestBootstrapScoutPromptAndSaveOnlyWhileScouting(t *testing.T) {
	h, _, bs := bootHooks(t)
	h.Config = fakeConfig{} // filter off: an unapproved command goes the normal way
	ctx := context.Background()
	if r := h.Handle(ctx, scoutPre()); strings.Contains(r.Prompt, "mem bootstrap save") {
		t.Errorf("prompt injected outside scouting: %q", r.Prompt)
	}
	if r := h.Handle(ctx, scoutSave("/bin/nxy")); r.AllowReason != "" {
		t.Errorf("save approved outside scouting: %+v", r)
	}
	bs.rows["repo:/p"] = BootstrapState{Repo: "repo:/p", OfferedAt: 1, Status: bootstrapScouting}
	r := h.Handle(ctx, scoutPre())
	if !strings.Contains(r.Prompt, "\"/bin/nxy\" mem bootstrap save") {
		t.Errorf("prompt = %q", r.Prompt)
	}
	if r := h.Handle(ctx, scoutSave("/bin/nxy")); r.AllowReason == "" {
		t.Errorf("save not approved while scouting: %+v", r)
	}
	other := scoutPre()
	other.AgentPrompt = "find the login"
	if r := h.Handle(ctx, other); strings.Contains(r.Prompt, "mem bootstrap save") {
		t.Errorf("another scout prompt got the bootstrap: %q", r.Prompt)
	}
}

func TestBootstrapAnswersAcrossPagesSaveRulesAndStartTheFeature(t *testing.T) {
	h, mem, bs := bootHooks(t)
	ctx := context.Background()
	h.Handle(ctx, prompt("/nxy:feature add a flag"))
	h.Handle(ctx, bootAnswerEv(map[string]string{bootstrapOfferQuestion: "Sí (recomendada)"}))
	if st := bs.rows["repo:/p"]; st.Status != bootstrapScouting {
		t.Fatalf("state after Sí = %+v", st)
	}
	st := bs.rows["repo:/p"]
	st.Status, st.Candidates = bootstrapConfirming, bootCands(6)
	bs.rows["repo:/p"] = st

	r := h.Handle(ctx, scoutDone())
	if r.Ask == nil || len(r.Ask.More) != 1 || !r.Ask.MultiSelect || !r.Ask.More[0].MultiSelect {
		t.Fatalf("scout return = %+v", r)
	}
	q1, q2 := memory.BootstrapQuestion(1, 2), memory.BootstrapQuestion(2, 2)
	if r.Ask.Question != q1 || r.Ask.More[0].Question != q2 {
		t.Errorf("questions = %q, %q", r.Ask.Question, r.Ask.More[0].Question)
	}
	r = h.Handle(ctx, bootAnswerEv(map[string]string{q1: "B2 regla b", q2: "B5 regla e, B6 regla f"}))
	if len(mem.added) != 3 {
		t.Fatalf("rules = %+v", mem.added)
	}
	for i, want := range []string{"regla b", "regla e", "regla f"} {
		got := mem.added[i]
		if got.Text != want || got.Origin != memory.OriginBootstrap || got.Question == "" || got.Repo != "repo:/p" {
			t.Errorf("rule %d = %+v", i, got)
		}
	}
	if r.Next == nil || r.Next.Feature.Request != "add a flag" || !strings.Contains(r.Context, "K1, K2, K3") {
		t.Fatalf("after answers = %+v", r)
	}
	if st := bs.rows["repo:/p"]; st.Status != bootstrapDone || st.PendingRequest != "" || st.Candidates != nil {
		t.Errorf("state = %+v", st)
	}
}

func TestBootstrapManualWithoutPendingRequest(t *testing.T) {
	h, mem, bs := bootHooks(t)
	ctx := context.Background()
	bs.rows["repo:/p"] = BootstrapState{Repo: "repo:/p", OfferedAt: 1, Status: bootstrapConfirming, Candidates: bootCands(1)}
	r := h.Handle(ctx, scoutDone())
	if r.Ask == nil || len(r.Ask.Options) != 2 {
		t.Fatalf("single candidate asks = %+v", r)
	}
	r = h.Handle(ctx, bootAnswerEv(map[string]string{memory.BootstrapQuestion(1, 1): "B1 regla a"}))
	if r.Next != nil || len(mem.added) != 1 || !strings.Contains(r.Context, "K1") {
		t.Fatalf("manual answer = %+v rules %+v", r, mem.added)
	}
}

func TestBootstrapScoutWithoutCandidatesStartsTheWaitingFeature(t *testing.T) {
	h, _, bs := bootHooks(t)
	ctx := context.Background()
	bs.rows["repo:/p"] = BootstrapState{Repo: "repo:/p", OfferedAt: 1, Status: bootstrapScouting, PendingRequest: "do it"}
	r := h.Handle(ctx, scoutDone())
	if r.Next == nil || r.Next.Feature.Request != "do it" {
		t.Fatalf("scout without candidates = %+v", r)
	}
	if st := bs.rows["repo:/p"]; st.Status != bootstrapDone {
		t.Errorf("state = %+v", st)
	}
}

func TestBootstrapOtherScoutDoesNotFinishIt(t *testing.T) {
	h, _, bs := bootHooks(t)
	bs.rows["repo:/p"] = BootstrapState{Repo: "repo:/p", OfferedAt: 1, Status: bootstrapScouting, PendingRequest: "do it"}
	ev := scoutDone()
	ev.AgentPrompt = "find the auth code"
	r := h.Handle(context.Background(), ev)
	if r.Ask != nil || r.Next != nil || bs.rows["repo:/p"].Status != bootstrapScouting {
		t.Errorf("other scout touched the bootstrap: %+v state %+v", r, bs.rows["repo:/p"])
	}
}

func TestBootstrapBackgroundScoutAsksForForeground(t *testing.T) {
	h, _, bs := bootHooks(t)
	bs.rows["repo:/p"] = BootstrapState{Repo: "repo:/p", OfferedAt: 1, Status: bootstrapScouting, PendingRequest: "do it"}
	ev := scoutDone()
	ev.AgentAsync = true
	r := h.Handle(context.Background(), ev)
	if r.Ask != nil || !strings.Contains(r.Context, "foreground") || bs.rows["repo:/p"].Status != bootstrapScouting {
		t.Errorf("background bootstrap scout = %+v state %+v", r, bs.rows["repo:/p"])
	}
}

func TestBootstrapBackgroundScoutIsLeftAlone(t *testing.T) {
	h, _, bs := bootHooks(t)
	bs.rows["repo:/p"] = BootstrapState{Repo: "repo:/p", OfferedAt: 1, Status: bootstrapConfirming, Candidates: bootCands(2)}
	ev := scoutDone()
	ev.AgentAsync = true
	if r := h.Handle(context.Background(), ev); r.Ask != nil {
		t.Errorf("background launch asked: %+v", r)
	}
}
