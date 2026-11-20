package app

import (
	"context"
	"strings"

	"github.com/mananos/nxy.dev/v2/internal/memory"
)

const (
	bootstrapScouting   = "scouting"
	bootstrapConfirming = "confirming"
	bootstrapAsked      = "asked"
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
	return HookResponse{Ask: offerAsk()}, true
}

// offerAsk is the question that offers the first rules.
func offerAsk() *AskSpec {
	return &AskSpec{
		Question: bootstrapOfferQuestion,
		Header:   "Reglas del repo",
		Options: []AskOption{
			{Label: bootstrapYes + " (recomendada)", Description: "Un scout lee el build, el CI y el README y propone hasta 10 reglas; vos elegís cuáles guardar"},
			{Label: bootstrapNo, Description: "Seguir con la feature sin reglas; no se vuelve a ofrecer"},
		},
		After: "wait for the answer: nxy continues with the feature request by itself, do not start it",
	}
}

// isBootstrapScout reports whether a scout prompt is the bootstrap's, original or rewritten by nxy.
func isBootstrapScout(prompt string) bool {
	p := strings.TrimSpace(prompt)
	return p == BootstrapPromptText || strings.HasPrefix(p, memory.BootstrapPromptHead)
}

// bootstrapPrompt is the scout's prompt when it is dispatched for the bootstrap, or "".
func (h Hooks) bootstrapPrompt(ctx context.Context, ev HookEvent) string {
	if h.Exe == "" || h.Memory == nil || !isBootstrapScout(ev.AgentPrompt) ||
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
		!isBootstrapScout(ev.AgentPrompt) {
		return HookResponse{}, false
	}
	_, repo := h.bootstrapRepo(ev)
	st, ok, err := h.Bootstrap.Bootstrap(ctx, repo)
	if err != nil || !ok {
		return HookResponse{}, false
	}
	if ev.AgentAsync {
		// A background scout is left to finish: nothing reaches the main thread when it ends, so the
		// Stop hook asks later, once the status is confirming.
		if st.Status == bootstrapScouting {
			return HookResponse{Context: "nxy bootstrap: the scout keeps running in the background; nxy asks which rules to keep when it ends. Do not launch it again."}, true
		}
		return HookResponse{}, false
	}
	switch st.Status {
	case bootstrapConfirming:
		return bootstrapAsk(st)
	case bootstrapScouting:
		return h.finishBootstrap(ctx, ev, st, "nxy bootstrap: el scout no propuso reglas."), true
	}
	return HookResponse{}, false
}

// bootstrapAsk is the question of the candidates to keep (false when there are none).
func bootstrapAsk(st BootstrapState) (HookResponse, bool) {
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
}

// markBootstrapAsked moves confirming to asked when the main thread asks one of the candidates' questions.
func (h Hooks) markBootstrapAsked(ctx context.Context, ev HookEvent) {
	if h.Bootstrap == nil || h.Repos == nil || ev.AgentID != "" {
		return
	}
	_, repo := h.bootstrapRepo(ev)
	st, ok, err := h.Bootstrap.Bootstrap(ctx, repo)
	if err != nil || !ok || st.Status != bootstrapConfirming {
		return
	}
	for _, a := range memory.BootstrapQuestions(st.Candidates) {
		for _, q := range ev.Questions {
			if q == a.Question {
				st.Status = bootstrapAsked
				_ = h.Bootstrap.SaveBootstrap(ctx, st)
				return
			}
		}
	}
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
	if st.Status != bootstrapConfirming && st.Status != bootstrapAsked {
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
	return h.saveChosen(ctx, ev, st, repo, memory.BootstrapChosen(ev.Answers, st.Candidates)), true
}

// saveChosen stores the chosen candidates as rules and closes the bootstrap.
func (h Hooks) saveChosen(ctx context.Context, ev HookEvent, st BootstrapState, repo string, chosen []memory.BootstrapCandidate) HookResponse {
	var saved []string
	if h.Memory != nil {
		for _, c := range chosen {
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
	return h.finishBootstrap(ctx, ev, st, text)
}

// bootstrapTextAnswer takes a typed message as the answer to the pending bootstrap question only
// when the whole message is the answer; pending says a bootstrap question is still waiting.
func (h Hooks) bootstrapTextAnswer(ctx context.Context, ev HookEvent) (r HookResponse, ok bool, pending *HookResponse) {
	if h.Bootstrap == nil || h.Repos == nil || ev.AgentID != "" {
		return HookResponse{}, false, nil
	}
	_, repo := h.bootstrapRepo(ev)
	st, found, err := h.Bootstrap.Bootstrap(ctx, repo)
	if err != nil || !found {
		return HookResponse{}, false, nil
	}
	text := strings.ToLower(strings.TrimSpace(ev.Prompt))
	switch {
	case st.Status == "" && st.PendingRequest != "":
		switch text {
		case "sí", "si":
			st.Status = bootstrapScouting
			if err := h.Bootstrap.SaveBootstrap(ctx, st); err != nil {
				return HookResponse{}, false, nil
			}
			return HookResponse{Context: BootstrapDispatch()}, true, nil
		case "no":
			return h.finishBootstrap(ctx, ev, st, "nxy bootstrap: sin reglas por ahora."), true, nil
		}
		return HookResponse{}, false, &HookResponse{Ask: offerAsk()}
	case st.Status == bootstrapConfirming || st.Status == bootstrapAsked:
		if chosen, isAnswer := memory.BootstrapChosenText(ev.Prompt, st.Candidates); isAnswer {
			return h.saveChosen(ctx, ev, st, repo, chosen), true, nil
		}
		if a, has := bootstrapAsk(st); has {
			return HookResponse{}, false, &a
		}
	}
	return HookResponse{}, false, nil
}
