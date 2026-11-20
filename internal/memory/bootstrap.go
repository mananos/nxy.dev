package memory

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// BootstrapMax is how many rule candidates a bootstrap may propose.
const BootstrapMax = 10

const (
	bootstrapPerQuestion = 4 // options of one AskUserQuestion question
	bootstrapMaxQuestion = 4 // questions of one AskUserQuestion call
	bootstrapNoneLabel   = "Ninguna"
)

// BootstrapCandidate is a rule the scout proposes for a repo that has none yet.
type BootstrapCandidate struct {
	Text     string   `json:"text"`
	Question string   `json:"question"`
	Evidence []string `json:"evidence"` // "path:line"
	Paths    []string `json:"paths,omitempty"`
}

var evidenceRE = regexp.MustCompile(`^[^\s:][^\s]*:\d+(-\d+)?$`)

// DecodeBootstrap reads the JSON array the scout saves: at most 10 candidates, each with text, the
// question it settles and evidence as path:line.
func DecodeBootstrap(data []byte) ([]BootstrapCandidate, error) {
	var out []BootstrapCandidate
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("bootstrap: not a JSON array of candidates: %w", err)
	}
	if len(out) == 0 {
		return nil, errors.New("bootstrap: no candidates")
	}
	if len(out) > BootstrapMax {
		return nil, fmt.Errorf("bootstrap: %d candidates, at most %d", len(out), BootstrapMax)
	}
	for i := range out {
		c := &out[i]
		c.Text = strings.TrimSpace(c.Text)
		c.Question = strings.TrimSpace(c.Question)
		if c.Text == "" {
			return nil, fmt.Errorf("bootstrap: candidate %d has no text", i+1)
		}
		if c.Question == "" {
			return nil, fmt.Errorf("bootstrap: candidate %d has no question (a rule must settle one)", i+1)
		}
		if len(c.Evidence) == 0 {
			return nil, fmt.Errorf("bootstrap: candidate %d has no evidence (path:line)", i+1)
		}
		for j, e := range c.Evidence {
			c.Evidence[j] = strings.TrimSpace(e)
			if !evidenceRE.MatchString(c.Evidence[j]) {
				return nil, fmt.Errorf("bootstrap: candidate %d: evidence %q is not path:line", i+1, e)
			}
		}
	}
	return out, nil
}

// BootstrapPromptHead is the first sentence of the bootstrap prompt: it tells the rewritten prompt apart.
const BootstrapPromptHead = "Propose the conventions of this repo that a newcomer would get wrong."

// BootstrapPrompt is the prompt of the scout that proposes the first rules of a repo. rules are the
// ones already saved (not to be repeated); exe is the nxy shim the scout saves with.
func BootstrapPrompt(rules []Rule, exe string) string {
	var b strings.Builder
	b.WriteString(BootstrapPromptHead)
	b.WriteString(" Read the build files, the linters' config, the CI workflows, README and CONTRIBUTING, and a sample of the code.\n")
	b.WriteString("Keep only what no tool enforces: if a linter or a test already fails on it, it is not a rule. For what a linter covers, the rule is \"run X before closing\" with the real command.\n")
	fmt.Fprintf(&b, "At most %d candidates, the most useful first. Each one has: text (one sentence, imperative), question (the question it settles, e.g. \"where do the migrations go?\"), evidence (a list of path:line where you saw it) and optionally paths (globs the rule is about).\n", BootstrapMax)
	if len(rules) > 0 {
		b.WriteString("Rules already saved (do not repeat them):\n")
		for _, r := range rules {
			fmt.Fprintf(&b, "- %s\n", clip(r.Text, 160))
		}
	}
	b.WriteString("Save them with one command, the JSON array in a heredoc, and nothing else:\n")
	fmt.Fprintf(&b, "\"%s\" mem bootstrap save <<'JSON'\n[{\"text\":\"...\",\"question\":\"...\",\"evidence\":[\"path/file:12\"],\"paths\":[\"src/**\"]}]\nJSON\n", exe)
	b.WriteString("If nothing qualifies, do not save anything: say so in one line.\n")
	return b.String()
}

// BootstrapOption is one option of a bootstrap question.
type BootstrapOption struct {
	Label       string
	Description string
}

// BootstrapAsk is one multiSelect question of the bootstrap offer.
type BootstrapAsk struct {
	Question string
	Header   string
	Options  []BootstrapOption
}

// BootstrapQuestion is the exact text of one bootstrap question (part of parts).
func BootstrapQuestion(part, parts int) string {
	return fmt.Sprintf("nxy bootstrap: ¿qué reglas guardo? (%d/%d)", part, parts)
}

func bootstrapLabel(i int, c BootstrapCandidate) string {
	return fmt.Sprintf("B%d %s", i+1, clip(c.Text, 50))
}

// BootstrapQuestions splits the candidates in multiSelect questions of 4 options (at most 4
// questions); a page with a single candidate gets a "Ninguna" option so it can be left empty.
func BootstrapQuestions(cands []BootstrapCandidate) []BootstrapAsk {
	if max := bootstrapPerQuestion * bootstrapMaxQuestion; len(cands) > max {
		cands = cands[:max]
	}
	parts := (len(cands) + bootstrapPerQuestion - 1) / bootstrapPerQuestion
	var out []BootstrapAsk
	for p := 0; p < parts; p++ {
		end := (p + 1) * bootstrapPerQuestion
		if end > len(cands) {
			end = len(cands)
		}
		q := BootstrapAsk{Question: BootstrapQuestion(p+1, parts), Header: fmt.Sprintf("Reglas %d/%d", p+1, parts)}
		for i := p * bootstrapPerQuestion; i < end; i++ {
			c := cands[i]
			desc := clip(c.Text, 120) + " · Resuelve: " + clip(c.Question, 100)
			if len(c.Evidence) > 0 {
				desc += " · Visto en " + strings.Join(c.Evidence, ", ")
			}
			q.Options = append(q.Options, BootstrapOption{Label: bootstrapLabel(i, c), Description: desc})
		}
		if len(q.Options) == 1 {
			q.Options = append(q.Options, BootstrapOption{Label: bootstrapNoneLabel, Description: "No guardar esta"})
		}
		out = append(out, q)
	}
	return out
}

var bootstrapPick = regexp.MustCompile(`(?i)^\s*["'“«]?\s*B(\d+)\b`)

// BootstrapChosen reads which candidates the answers pick (answers are keyed by the question text),
// in candidate order, once each. An option counts by its exact label or by the B<n> that starts a
// comma-separated part of the answer.
func BootstrapChosen(answers map[string]string, cands []BootstrapCandidate) []BootstrapCandidate {
	asks := BootstrapQuestions(cands)
	picked := map[int]bool{}
	for p, a := range asks {
		answer, ok := answers[a.Question]
		if !ok {
			continue
		}
		lo, hi := p*bootstrapPerQuestion, (p+1)*bootstrapPerQuestion
		if hi > len(cands) {
			hi = len(cands)
		}
		rest := answer
		for i := lo; i < hi; i++ {
			if label := bootstrapLabel(i, cands[i]); strings.Contains(rest, label) {
				picked[i] = true
				rest = strings.Replace(rest, label, "", 1)
			}
		}
		for _, part := range strings.Split(rest, ",") {
			if m := bootstrapPick.FindStringSubmatch(part); m != nil {
				var n int
				fmt.Sscanf(m[1], "%d", &n)
				if n > lo && n <= hi {
					picked[n-1] = true
				}
			}
		}
	}
	var out []BootstrapCandidate
	for i, c := range cands {
		if picked[i] {
			out = append(out, c)
		}
	}
	return out
}

var bootstrapTextPick = regexp.MustCompile(`(?i)^B(\d+)(\s*,\s*B(\d+))*$`)
var bootstrapTextNum = regexp.MustCompile(`(?i)B(\d+)`)

// BootstrapChosenText reads a typed answer to the candidates question. It counts only when the whole
// message is B<n>[, B<n>…] with every n in range, "todas" or "ninguna"; ok is false for anything else.
func BootstrapChosenText(text string, cands []BootstrapCandidate) ([]BootstrapCandidate, bool) {
	t := strings.TrimSpace(text)
	switch strings.ToLower(t) {
	case "todas":
		return append([]BootstrapCandidate(nil), cands...), true
	case "ninguna":
		return nil, true
	}
	if !bootstrapTextPick.MatchString(t) {
		return nil, false
	}
	picked := map[int]bool{}
	for _, m := range bootstrapTextNum.FindAllStringSubmatch(t, -1) {
		n, err := strconv.Atoi(m[1])
		if err != nil || n < 1 || n > len(cands) {
			return nil, false
		}
		picked[n-1] = true
	}
	var out []BootstrapCandidate
	for i, c := range cands {
		if picked[i] {
			out = append(out, c)
		}
	}
	return out, true
}
