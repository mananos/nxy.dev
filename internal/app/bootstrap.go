package app

import (
	"context"
	"strings"

	"github.com/mananos/nxy.dev/v2/internal/memory"
)

const (
	bootstrapScouting   = "scouting"
	bootstrapConfirming = "confirming"
	bootstrapDone       = "done"

	// BootstrapPromptText is the prompt the scout is dispatched with for the bootstrap.
	BootstrapPromptText = "nxy bootstrap"

	bootstrapOfferQuestion = "Este repo no tiene reglas guardadas. ¿Las propongo ahora? (~30–60k tokens, 1–2 min, una sola vez)"
	bootstrapYes           = "Sí"
	bootstrapNo            = "No"
)

// BootstrapDispatch is what the main thread is told to do to start the bootstrap.
func BootstrapDispatch() string {
	return "nxy: dispatch the Agent tool with subagent_type \"nxy:scout\" and prompt exactly \"" + BootstrapPromptText +
		"\", in the foreground (not in the background), and nothing else. nxy writes the scout's instructions and then asks the user which rules to keep."
}

// bootstrapRepo is the dir and repo key of the event.
func (h Hooks) bootstrapRepo(ev HookEvent) (dir, repo string) {
	dir = ev.ProjectDir
	if dir == "" {
		dir = ev.Cwd
	}
	return dir, h.Repos.Key(dir)
}

// bootstrapStatus is the bootstrap status of the event's repo ("" when there is none).
func (h Hooks) bootstrapStatus(ctx context.Context, ev HookEvent) string {
	if h.Bootstrap == nil || h.Repos == nil {
		return ""
	}
	_, repo := h.bootstrapRepo(ev)
	st, ok, err := h.Bootstrap.Bootstrap(ctx, repo)
	if err != nil || !ok {
		return ""
	}
	return st.Status
}

// offerBootstrap asks, once per repo, whether to propose the first rules when a feature starts in a
// repo without any. The request waits in the bootstrap row until the user answers.
func (h Hooks) offerBootstrap(ctx context.Context, repo, request string, engineer bool) (HookResponse, bool) {
	if h.Bootstrap == nil || h.Memory == nil {
		return HookResponse{}, false
	}
	rules, err := h.Memory.Rules(ctx, repo)
	if err != nil || len(rules) > 0 {
		return HookResponse{}, false
	}
	first, err := h.Bootstrap.MarkOffered(ctx, repo)
	if err != nil || !first {
		return HookResponse{}, false
	}
	st, _, err := h.Bootstrap.Bootstrap(ctx, repo)
	if err != nil {
		return HookResponse{}, false
	}
	st.Repo, st.Status, st.PendingRequest, st.PendingEngineer = repo, "", request, engineer
	if err := h.Bootstrap.SaveBootstrap(ctx, st); err != nil {
		return HookResponse{}, false
	}
	return HookResponse{Ask: &AskSpec{
		Question: bootstrapOfferQuestion,
		Header:   "Reglas del repo",
		Options: []AskOption{
			{Label: bootstrapYes + " (recomendada)", Description: "Un scout lee el build, el CI y el README y propone hasta 10 reglas; vos elegís cuáles guardar"},
			{Label: bootstrapNo, Description: "Seguir con la feature sin reglas; no se vuelve a ofrecer"},
		},
		After: "wait for the answer: nxy continues with the feature request by itself, do not start it",
	}}, true
}

// bootstrapPrompt is the scout's prompt when it is dispatched for the bootstrap, or "".
func (h Hooks) bootstrapPrompt(ctx context.Context, ev HookEvent) string {
	if h.Exe == "" || h.Memory == nil || strings.TrimSpace(ev.AgentPrompt) != BootstrapPromptText ||
		h.bootstrapStatus(ctx, ev) != bootstrapScouting {
		return ""
	}
	_, repo := h.bootstrapRepo(ev)
	rules, _ := h.Memory.Rules(ctx, repo)
	return memory.BootstrapPrompt(rules, strings.ReplaceAll(h.Exe, `\`, "/"))
}

// parseBootstrapSave reports whether cmd is `"<exe>" mem bootstrap save` with its JSON heredoc.
func (h Hooks) parseBootstrapSave(cmd string, ps bool) bool {
	return h.parseSave(cmd, ps, func(toks []string) string {
		if len(toks) != 4 || !h.isExe(toks[0]) || toks[1] != "mem" || toks[2] != "bootstrap" || toks[3] != "save" {
			return ""
		}
		return "bootstrap"
	}) != ""
}

// bootstrapScoutDone handles the return of the bootstrap scout (foreground): with candidates saved
// it asks which to keep; with none it closes the bootstrap and goes on with the waiting request.
func (h Hooks) bootstrapScoutDone(ctx context.Context, ev HookEvent) (HookResponse, bool) {
	if h.Bootstrap == nil || h.Repos == nil || ev.AgentID != "" || ev.Subagent != roleScout ||
		strings.TrimSpace(ev.AgentPrompt) != BootstrapPromptText {
		return HookResponse{}, false
	}
	_, repo := h.bootstrapRepo(ev)
	st, ok, err := h.Bootstrap.Bootstrap(ctx, repo)
	if err != nil || !ok {
		return HookResponse{}, false
	}
	if ev.AgentAsync {
		// deliverPending carries only a feature step, not a question, and nothing reaches the main
		// thread when a background agent ends: the bootstrap is foreground-only. The status stays
		// "scouting", so running it again in the foreground works.
		if st.Status == bootstrapScouting {
			return HookResponse{Context: "nxy bootstrap: the scout must run in the foreground, not in the background. " + BootstrapDispatch()}, true
		}
		return HookResponse{}, false
	}
	switch st.Status {
	case bootstrapConfirming:
		asks := memory.BootstrapQuestions(st.Candidates)
		if len(asks) == 0 {
			return HookResponse{}, false
		}
		var specs []AskSpec
		for _, a := range asks {
			s := AskSpec{Question: a.Question, Header: a.Header, MultiSelect: true}
			for _, o := range a.Options {
				s.Options = append(s.Options, AskOption{Label: o.Label, Description: o.Description})
			}
			specs = append(specs, s)
		}
		first := specs[0]
		first.More = specs[1:]
		first.After = "and wait for the answers: nxy saves the chosen rules"
		return HookResponse{Ask: &first}, true
	case bootstrapScouting:
		return h.finishBootstrap(ctx, ev, st, "nxy bootstrap: el scout no propuso reglas."), true
	}
	return HookResponse{}, false
}

// finishBootstrap marks the bootstrap done and, if a request was waiting, creates its feature.
func (h Hooks) finishBootstrap(ctx context.Context, ev HookEvent, st BootstrapState, text string) HookResponse {
	request, engineer := st.PendingRequest, st.PendingEngineer
	st.Status, st.Candidates, st.PendingRequest, st.PendingEngineer = bootstrapDone, nil, "", false
	_ = h.Bootstrap.SaveBootstrap(ctx, st)
	if request == "" || h.Store == nil {
		return HookResponse{Context: text}
	}
	dir, _ := h.bootstrapRepo(ev)
	r := h.createFeature(ctx, ev, dir, request, engineer)
	if r.Context != "" {
		text += "\n" + r.Context
	}
	r.Context = text
	return r
}

// answerBootstrap turns the answers to the bootstrap questions (the offer, then the candidates)
// into state. ok is false when the answers are not to those questions.
func (h Hooks) answerBootstrap(ctx context.Context, ev HookEvent) (HookResponse, bool) {
	if h.Bootstrap == nil || h.Repos == nil {
		return HookResponse{}, false
	}
	_, repo := h.bootstrapRepo(ev)
	st, ok, err := h.Bootstrap.Bootstrap(ctx, repo)
	if err != nil || !ok {
		return HookResponse{}, false
	}
	if answer, found := ev.Answers[bootstrapOfferQuestion]; found {
		if st.Status != "" || st.PendingRequest == "" {
			return HookResponse{}, true
		}
		if strings.HasPrefix(strings.TrimSpace(answer), bootstrapYes) {
			st.Status = bootstrapScouting
			if err := h.Bootstrap.SaveBootstrap(ctx, st); err != nil {
				return HookResponse{}, true
			}
			return HookResponse{Context: BootstrapDispatch()}, true
		}
		return h.finishBootstrap(ctx, ev, st, "nxy bootstrap: sin reglas por ahora."), true
	}
	if st.Status != bootstrapConfirming {
		return HookResponse{}, false
	}
	answered := false
	for _, a := range memory.BootstrapQuestions(st.Candidates) {
		if _, found := ev.Answers[a.Question]; found {
			answered = true
		}
	}
	if !answered {
		return HookResponse{}, false
	}
	var saved []string
	if h.Memory != nil {
		for _, c := range memory.BootstrapChosen(ev.Answers, st.Candidates) {
			r, err := h.Memory.AddRule(ctx, memory.Rule{Repo: repo, Text: c.Text, Question: c.Question, Origin: memory.OriginBootstrap, Paths: c.Paths})
			if err == nil {
				saved = append(saved, r.Short)
			}
		}
	}
	text := "nxy bootstrap: no se guardó ninguna regla."
	if len(saved) > 0 {
		text = "nxy bootstrap: guardadas " + strings.Join(saved, ", ") + "."
	}
	return h.finishBootstrap(ctx, ev, st, text), true
}
