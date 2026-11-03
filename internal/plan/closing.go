package plan

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/mananos/nxy.dev/internal/domain"
)

const (
	// RuleMaxGroups and RuleMaxOptions are the limits of one AskUserQuestion call.
	RuleMaxGroups  = 4
	RuleMaxOptions = 4
	// RuleNoneLabel is the extra option of a group with a single candidate.
	RuleNoneLabel = "Ninguna"
)

// RuleGroup is one multiSelect question of the closing question.
type RuleGroup struct {
	Header  string
	Options []domain.Option
}

// RulesQuestionPrefix starts the text of every rules question of a feature.
func RulesQuestionPrefix(id string) string {
	return fmt.Sprintf("nxy feature %s: ¿qué guardar como regla del repo?", id)
}

// RulesQuestion is the exact text of one rules question (part of parts).
func RulesQuestion(id string, part, parts int) string {
	return fmt.Sprintf("%s (%d/%d)", RulesQuestionPrefix(id), part, parts)
}

// RuleShown cuts the offer to what fits in one AskUserQuestion call.
func RuleShown(offer []domain.RuleCandidate) []domain.RuleCandidate {
	if max := RuleMaxGroups * RuleMaxOptions; len(offer) > max {
		return offer[:max]
	}
	return offer
}

// RuleGroups splits the offer in at most 4 questions of at most 4 options; a group of one gets a
// "Ninguna" option so it can be answered with nothing.
func RuleGroups(offer []domain.RuleCandidate) []RuleGroup {
	shown := RuleShown(offer)
	n := (len(shown) + RuleMaxOptions - 1) / RuleMaxOptions
	var groups []RuleGroup
	for i := 0; i < n; i++ {
		end := (i + 1) * RuleMaxOptions
		if end > len(shown) {
			end = len(shown)
		}
		g := RuleGroup{Header: fmt.Sprintf("Reglas %d/%d", i+1, n)}
		for _, c := range shown[i*RuleMaxOptions : end] {
			desc := "Resuelve: " + c.Question
			if c.Supersedes != "" {
				desc += " · reemplaza a " + c.Supersedes
				if c.SupersedesText != "" {
					desc += ": " + c.SupersedesText
				}
			}
			g.Options = append(g.Options, domain.Option{Label: c.ID + " " + c.Text, Description: desc})
		}
		if len(g.Options) == 1 {
			g.Options = append(g.Options, domain.Option{Label: RuleNoneLabel, Description: "No guardar esta"})
		}
		groups = append(groups, g)
	}
	return groups
}

var leadingIDRE = regexp.MustCompile(`(?i)^\s*S(\d+)\b`)

// ChosenRuleIDs reads which offered candidates an answer picks, in offer order, once each. An
// option counts by its exact label ("S2 <text>") or by the S<n> that starts a comma-separated part
// of the answer; an S<n> inside the free text of a rule is not a pick.
func ChosenRuleIDs(answer string, offer []domain.RuleCandidate) []string {
	picked := map[string]bool{}
	rest := answer
	for _, c := range offer {
		if label := c.ID + " " + c.Text; c.ID != "" && strings.Contains(rest, label) {
			picked[c.ID] = true
			rest = strings.Replace(rest, label, "", 1)
		}
	}
	for _, part := range strings.Split(rest, ",") {
		if m := leadingIDRE.FindStringSubmatch(part); m != nil {
			picked["S"+m[1]] = true
		}
	}
	var out []string
	for _, c := range offer {
		if picked[c.ID] {
			out = append(out, c.ID)
		}
	}
	return out
}

// IsNoneRules reports whether an answer is the "Ninguna" option.
func IsNoneRules(answer string) bool {
	return strings.Contains(strings.ToLower(answer), strings.ToLower(RuleNoneLabel))
}
