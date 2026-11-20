package app

import (
	"context"
	"slices"
	"time"

	"github.com/mananos/nxy.dev/v2/internal/ask"
	"github.com/mananos/nxy.dev/v2/internal/design"
	"github.com/mananos/nxy.dev/v2/internal/domain"
)

// Who answered a held question (pending_asks.answered_by).
const (
	answeredByTUI      = "tui"
	answeredByTerminal = "terminal"
	answeredByStopped  = "stopped"
)

// tuiQuestions are the questions of f the TUI knows how to show and answer: the ones ask.For builds
// (except saving a decision and closing the design, which are answered in the chat) and the question
// of the open fork with the labels saved by design fork.
func tuiQuestions(f domain.Feature) []domain.AskedQuestion {
	var out []domain.AskedQuestion
	if a, ok := ask.For(f); ok && a.Kind != ask.KindDesignProposal && a.Kind != ask.KindDesignClose {
		for _, q := range a.Questions {
			aq := domain.AskedQuestion{Question: q.Question, Header: q.Header, MultiSelect: q.MultiSelect}
			for _, o := range q.Options {
				aq.Options = append(aq.Options, o.Label)
			}
			out = append(out, aq)
		}
	}
	if f.Design != nil && f.Design.Current != "" {
		for _, fk := range f.Design.Agenda {
			if fk.ID != f.Design.Current || len(fk.Options) == 0 {
				continue
			}
			aq := domain.AskedQuestion{Question: design.ForkQuestion(f.ID, fk.ID)}
			for _, o := range fk.Options {
				aq.Options = append(aq.Options, o.Label)
			}
			out = append(out, aq)
		}
	}
	return out
}

// tuiAnswerable is true when every asked question is one the TUI shows, with the same text and labels.
func tuiAnswerable(f domain.Feature, asked []domain.AskedQuestion) bool {
	if len(asked) == 0 {
		return false
	}
	known := tuiQuestions(f)
	for _, q := range asked {
		found := false
		for _, k := range known {
			if k.Question == q.Question && slices.Equal(k.Options, q.Options) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// waitInTUI holds the AskUserQuestion call of the main thread while a live TUI can answer it: it
// writes the question down and rereads it until the TUI answers, the user takes it to the terminal,
// the TUI closes or the wait runs out. Those last three leave the normal dialog (empty response).
func (h Hooks) waitInTUI(ctx context.Context, ev HookEvent, f domain.Feature) HookResponse {
	if h.Asks == nil || h.TUI == nil || ev.AgentID != "" || !h.TUI.Alive() || !tuiAnswerable(f, ev.Asked) {
		return HookResponse{}
	}
	row, err := h.Asks.CreatePendingAsk(ctx, domain.PendingAsk{
		SessionID: ev.SessionID, FeatureID: f.ID, ToolUseID: ev.ToolUseID, Questions: ev.Asked,
	})
	if err != nil {
		return HookResponse{}
	}
	defer func() { _ = h.Asks.DeletePendingAsk(context.WithoutCancel(ctx), row.ID) }()
	start := time.Now()
	for {
		_ = h.Asks.TouchPendingAsk(ctx, row.ID)
		got, err := h.Asks.PendingAsk(ctx, row.ID)
		if err != nil {
			return HookResponse{}
		}
		if got.Answers != nil {
			switch {
			case got.AnsweredBy == answeredByStopped:
				return HookResponse{Deny: "nxy: the user stopped feature " + f.ID + " from the TUI"}
			case got.AnsweredBy == answeredByTerminal, len(got.Answers) != len(ev.Asked):
				return HookResponse{}
			}
			answers := make(map[string]string, len(ev.Asked))
			for i, q := range ev.Asked {
				answers[q.Question] = got.Answers[i]
			}
			return HookResponse{Answers: &AskAnswers{ByQuestion: answers}}
		}
		if !h.TUI.Alive() || time.Since(start) >= h.AskWait.max() {
			return HookResponse{}
		}
		select {
		case <-ctx.Done():
			return HookResponse{}
		case <-time.After(h.AskWait.every()):
		}
	}
}
