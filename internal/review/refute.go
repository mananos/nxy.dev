package review

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/mananos/nxy.dev/internal/domain"
)

// VerdictEntry is the refuter's conclusion about one finding.
type VerdictEntry struct {
	ID      string         `json:"id"`
	Verdict domain.Verdict `json:"verdict"`
	Why     string         `json:"why,omitempty"`
}

// Verdicts is what the refuter saves.
type Verdicts struct {
	Verdicts []VerdictEntry `json:"verdicts"`
}

// DecodeVerdicts reads the refuter's answer; an unknown field, a repeated id or
// an invalid verdict is an error.
func DecodeVerdicts(r io.Reader) (Verdicts, error) {
	var v Verdicts
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		return Verdicts{}, fmt.Errorf("refute JSON: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Verdicts{}, errors.New("refute JSON: extra content after the object")
	}
	seen := map[string]bool{}
	for i, e := range v.Verdicts {
		switch e.Verdict {
		case domain.VerdictConfirmed, domain.VerdictRefuted, domain.VerdictInconclusive:
		default:
			return Verdicts{}, fmt.Errorf("verdicts[%d]: %q is not confirmed, refuted or inconclusive", i, e.Verdict)
		}
		if seen[e.ID] {
			return Verdicts{}, fmt.Errorf("verdicts[%d]: %s sent more than once", i, e.ID)
		}
		seen[e.ID] = true
	}
	return v, nil
}

func needsVerdict(f domain.Finding) bool {
	return f.Severity == domain.SeverityHigh && f.Cause == domain.CauseChange && f.Dropped == "" && f.Verdict == ""
}

// ApplyVerdicts sets the verdicts on the findings and marks the review refuted.
// An id that is not a pending high finding is an error; a pending one with no
// verdict is left inconclusive.
func ApplyVerdicts(rv domain.Review, vs Verdicts) (domain.Review, error) {
	byID := map[string]VerdictEntry{}
	for _, e := range vs.Verdicts {
		byID[e.ID] = e
	}
	pending := map[string]bool{}
	fs := append([]domain.Finding(nil), rv.Findings...)
	for _, f := range fs {
		if needsVerdict(f) {
			pending[f.ID] = true
		}
	}
	for id := range byID {
		if !pending[id] {
			return rv, fmt.Errorf("%s is not a high finding waiting for a verdict", id)
		}
	}
	for i, f := range fs {
		if !pending[f.ID] {
			continue
		}
		if e, ok := byID[f.ID]; ok {
			fs[i].Verdict, fs[i].VerdictWhy = e.Verdict, e.Why
		} else {
			fs[i].Verdict = domain.VerdictInconclusive
		}
	}
	rv.Findings = fs
	rv.Refuted = true
	return rv, nil
}

// MissingInconclusive is for a refuter that came back without saving: every
// pending high finding stays inconclusive (and is still offered).
func MissingInconclusive(rv domain.Review) domain.Review {
	rv, _ = ApplyVerdicts(rv, Verdicts{})
	return rv
}
