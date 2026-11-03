package design

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mananos/nxy.dev/internal/domain"
)

const maxText = 200

// agendaInput is what the main thread sends to set the forks of the design.
type agendaInput struct {
	Forks []string `json:"forks"`
}

// ProposalInput is one decision the main thread asks the user to save.
type ProposalInput struct {
	Fork    string       `json:"fork"`
	Text    string       `json:"text"`
	Why     string       `json:"why"`
	Dissent string       `json:"dissent,omitempty"`
	Scope   domain.Scope `json:"scope"`
}

func decodeStrict(r io.Reader, what string, v any) error {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("%s JSON: %w", what, err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%s JSON: extra content after the object", what)
	}
	return nil
}

// DecodeAgenda reads {"forks": ["title", ...]}.
func DecodeAgenda(r io.Reader) ([]string, error) {
	var a agendaInput
	if err := decodeStrict(r, "agenda", &a); err != nil {
		return nil, err
	}
	return a.Forks, nil
}

// DecodeProposal reads one proposed decision.
func DecodeProposal(r io.Reader) (ProposalInput, error) {
	var p ProposalInput
	if err := decodeStrict(r, "proposal", &p); err != nil {
		return ProposalInput{}, err
	}
	return p, nil
}

// DecodeClose reads the close of the design.
func DecodeClose(r io.Reader) (domain.DesignClose, error) {
	var c domain.DesignClose
	if err := decodeStrict(r, "close", &c); err != nil {
		return domain.DesignClose{}, err
	}
	return c, nil
}

func oneLine(s string) bool { return !strings.ContainsAny(s, "\r\n") && !strings.Contains(s, "```") }

// nextNum returns the next number for ids with the given prefix.
func nextNum(ids []string, prefix string) int {
	max := 0
	for _, id := range ids {
		if n, err := strconv.Atoi(strings.TrimPrefix(id, prefix)); err == nil && n > max {
			max = n
		}
	}
	return max + 1
}

func forkIDs(d *domain.Design) []string {
	ids := make([]string, 0, len(d.Agenda))
	for _, f := range d.Agenda {
		ids = append(ids, f.ID)
	}
	return ids
}

// SetAgenda replaces the open forks with titles; decided and delegated ones stay.
func SetAgenda(d *domain.Design, titles []string) []domain.Problem {
	var out []domain.Problem
	if d.Pending != nil || d.Close != nil {
		out = append(out, domain.Problem{Where: "forks", Msg: "there is a proposal or close waiting for the user's answer"})
	}
	if len(titles) == 0 {
		out = append(out, domain.Problem{Where: "forks", Msg: "at least one fork is needed"})
	}
	for i, t := range titles {
		where := fmt.Sprintf("forks[%d]", i)
		if strings.TrimSpace(t) == "" {
			out = append(out, domain.Problem{Where: where, Msg: "empty title"})
		} else if !oneLine(t) || utf8.RuneCountInString(t) > maxText {
			out = append(out, domain.Problem{Where: where, Msg: fmt.Sprintf("title must be one line of at most %d characters", maxText)})
		}
	}
	if len(out) > 0 {
		return out
	}
	// Replaced open forks leave their ids behind: a new fork never reuses one.
	n := nextNum(forkIDs(d), "B")
	kept := d.Agenda[:0:0]
	for _, f := range d.Agenda {
		if f.Status != domain.ForkOpen {
			kept = append(kept, f)
		}
	}
	d.Agenda = kept
	for _, t := range titles {
		d.Agenda = append(d.Agenda, domain.Fork{ID: "B" + strconv.Itoa(n), Title: strings.TrimSpace(t), Status: domain.ForkOpen, Source: domain.SourceAgenda})
		n++
	}
	return nil
}

// AddForks appends open forks with new ids.
func AddForks(d *domain.Design, titles []string, source domain.ForkSource) {
	n := nextNum(forkIDs(d), "B")
	for _, t := range titles {
		d.Agenda = append(d.Agenda, domain.Fork{ID: "B" + strconv.Itoa(n), Title: strings.TrimSpace(t), Status: domain.ForkOpen, Source: source})
		n++
	}
}

func findFork(d *domain.Design, id string) *domain.Fork {
	for i := range d.Agenda {
		if d.Agenda[i].ID == id {
			return &d.Agenda[i]
		}
	}
	return nil
}

// ValidateProposal checks a proposal against the design.
func ValidateProposal(d *domain.Design, p ProposalInput) []domain.Problem {
	var out []domain.Problem
	add := func(where, format string, a ...any) {
		out = append(out, domain.Problem{Where: where, Msg: fmt.Sprintf(format, a...)})
	}
	if d.Pending != nil {
		add("proposal", "there is already a proposal waiting for the user's answer")
	}
	if f := findFork(d, p.Fork); f == nil {
		add("fork", "%q is not in the agenda", p.Fork)
	} else if f.Status != domain.ForkOpen {
		add("fork", "%s is already %s", f.ID, f.Status)
	}
	if strings.TrimSpace(p.Text) == "" {
		add("text", "empty")
	} else if !oneLine(p.Text) || utf8.RuneCountInString(p.Text) > maxText {
		add("text", "must be one line of at most %d characters", maxText)
	}
	if strings.TrimSpace(p.Why) == "" {
		add("why", "empty: say why")
	}
	if p.Scope != domain.ScopeRepo && p.Scope != domain.ScopeTask {
		add("scope", "must be repo or task")
	}
	return out
}

// Propose leaves p waiting for the user's OK.
func Propose(d *domain.Design, p ProposalInput) {
	d.Pending = &domain.Decision{
		Scope: p.Scope, Text: strings.TrimSpace(p.Text), Why: strings.TrimSpace(p.Why),
		Dissent: strings.TrimSpace(p.Dissent), Fork: p.Fork,
	}
}

// Approve moves the pending proposal to the decisions and marks its fork decided.
func Approve(d *domain.Design) (domain.Decision, bool) {
	if d.Pending == nil {
		return domain.Decision{}, false
	}
	ids := make([]string, 0, len(d.Decisions))
	for _, x := range d.Decisions {
		ids = append(ids, x.ID)
	}
	dec := *d.Pending
	dec.ID = "D" + strconv.Itoa(nextNum(ids, "D"))
	d.Decisions = append(d.Decisions, dec)
	d.Pending = nil
	if f := findFork(d, dec.Fork); f != nil {
		f.Status = domain.ForkDecided
	}
	return dec, true
}

// Discard drops the pending proposal (the user wants to keep debating).
func Discard(d *domain.Design) { d.Pending = nil }

// ValidateClose checks the close proposed by the main thread.
func ValidateClose(d *domain.Design, c domain.DesignClose) []domain.Problem {
	var out []domain.Problem
	add := func(where, format string, a ...any) {
		out = append(out, domain.Problem{Where: where, Msg: fmt.Sprintf(format, a...)})
	}
	if d.Pending != nil {
		add("close", "there is a proposal waiting for the user's answer")
	}
	if strings.Contains(c.D2, "```") {
		add("d2", "must not contain code fences (```)")
	}
	if len(c.Criteria) == 0 {
		add("criteria", "at least one criterion is needed")
	}
	for i, r := range c.Criteria {
		where := fmt.Sprintf("criteria[%d]", i)
		if want := "R" + strconv.Itoa(i+1); r.ID != want {
			add(where+".id", "must be %s", want)
		}
		if strings.TrimSpace(r.Text) == "" {
			add(where+".text", "empty")
		} else if !oneLine(r.Text) {
			add(where+".text", "must be one line without code fences")
		}
	}
	if !c.Delegate {
		for _, f := range d.Agenda {
			if f.Status == domain.ForkOpen {
				add("close", "%s (%s) is still open: decide it or close with delegate", f.ID, f.Title)
			}
		}
	}
	return out
}

// ApplyClose records the close; open forks become delegated when c.Delegate.
func ApplyClose(d *domain.Design, c domain.DesignClose) {
	d.Criteria = c.Criteria
	d.D2 = c.D2
	d.Delegate = c.Delegate
	d.Close = nil
	d.Closed = true
	if c.Delegate {
		for i := range d.Agenda {
			if d.Agenda[i].Status == domain.ForkOpen {
				d.Agenda[i].Status = domain.ForkDelegated
			}
		}
	}
}
