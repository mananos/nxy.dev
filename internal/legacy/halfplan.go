package legacy

import (
	"crypto/sha1"
	"encoding/hex"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/mananos/nxy.dev/internal/domain"
)

// ScopeHalfPlan is the scope of the mark of a 1.x plan left half-finished (key = HalfPlanKey);
// its answer is one of the Answer* values.
const ScopeHalfPlan = "plan-1x"

// Answers kept in the mark of a half-finished 1.x plan.
const (
	AnswerReplan = "replan"
	AnswerIgnore = "ignore"
	AnswerClosed = "closed" // nothing was left to do in the 1.x: never offered
)

// Option labels of the question about a half-finished 1.x plan.
const (
	HalfPlanReplan = "Planificarla de nuevo"
	HalfPlanIgnore = "Ignorarla"
)

// PlanFiles is what the 1.x left in .nxy/local about plans: the approved hashes
// (plan-approvals.json) and the status of each batch of verify/<hash>/batch-<n>.json.
type PlanFiles struct {
	Approved []string
	Verify   map[string]map[int]string // hash -> batch -> status
}

// HalfPlan is a 1.x plan, approved, with some batch not yet done.
type HalfPlan struct {
	Hash    string
	Goal    string
	Plan    string
	Open    []int // batches not done, in plan order
	Started bool  // some batch has a verify file
	Updated int64 // of the handoff that carries the plan
}

var (
	planHeadRe = regexp.MustCompile(`(?i)^##\s*Plan\s*$`)
	h2Re       = regexp.MustCompile(`^##\s`)
	goalRe     = regexp.MustCompile(`(?im)^Goal:[ \t]*(.*)$`)
	h3SplitRe  = regexp.MustCompile(`(?m)^###\s+`)
	skipSecRe  = regexp.MustCompile(`(?i)^(Questions|Repos)\s*$`)
	batchNumRe = regexp.MustCompile(`(?i)^Batch\s+(\d+)`)
	hashNameRe = regexp.MustCompile(`^[0-9a-z]+$`)
)

// ExtractPlan is the "## Plan" section of a handoff body (heading included, up to the next "## "
// heading), or "" when there is none. Port of the 1.x extractPlan.
func ExtractPlan(body string) string {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	start := -1
	for i, l := range lines {
		if planHeadRe.MatchString(l) {
			start = i
			break
		}
	}
	if start < 0 {
		return ""
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if h2Re.MatchString(lines[i]) {
			end = i
			break
		}
	}
	return strings.TrimSpace(strings.Join(lines[start:end], "\n"))
}

// PlanHash is the 1.x hash of a plan: whitespace-insensitive, sha1 of the trimmed non-blank lines, 8 hex digits.
func PlanHash(plan string) string {
	var keep []string
	for _, l := range strings.Split(strings.ReplaceAll(plan, "\r\n", "\n"), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			keep = append(keep, l)
		}
	}
	sum := sha1.Sum([]byte(strings.Join(keep, "\n")))
	return hex.EncodeToString(sum[:])[:8]
}

// PlanGoal is the plan's "Goal:" line, or "".
func PlanGoal(plan string) string {
	m := goalRe.FindStringSubmatch(strings.ReplaceAll(plan, "\r\n", "\n"))
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m[1])
}

// PlanBatches is the numbers of the plan's batches, as written ("### Batch 2 — ...") or by position.
func PlanBatches(plan string) []int {
	parts := h3SplitRe.Split(strings.ReplaceAll(plan, "\r\n", "\n"), -1)
	var out []int
	for _, s := range parts[1:] {
		first := strings.SplitN(s, "\n", 2)[0]
		title := strings.TrimSpace(first)
		if skipSecRe.MatchString(strings.TrimRight(first, " \t\r")) {
			continue
		}
		n := len(out) + 1
		if m := batchNumRe.FindStringSubmatch(title); m != nil {
			n, _ = strconv.Atoi(m[1])
		}
		out = append(out, n)
	}
	return out
}

// ValidPlanHash tells whether name looks like a plan hash (the name of a verify/<hash> folder).
func ValidPlanHash(name string) bool { return hashNameRe.MatchString(name) }

func batchDone(status string) bool {
	return status == "pass" || status == "manual" || status == "none"
}

// Candidates are the hashes the 1.x approved or started verifying, minus the ones answered, sorted.
func Candidates(files PlanFiles, answered func(hash string) bool) []string {
	seen := map[string]bool{}
	var out []string
	add := func(h string) {
		if h == "" || seen[h] || (answered != nil && answered(h)) {
			return
		}
		seen[h] = true
		out = append(out, h)
	}
	for _, h := range files.Approved {
		add(h)
	}
	for h := range files.Verify {
		add(h)
	}
	sort.Strings(out)
	return out
}

// FindHalfPlans looks, among the candidate hashes, for plans carried by a live 1.x handoff
// (type handoff, id without "-done-", scope project or area) that still have a batch not done.
// open is newest handoff first; closed are the candidates with nothing left to offer
// (no live handoff with that plan, or every batch done).
func FindHalfPlans(mems []Memory, files PlanFiles, cands []string) (open []HalfPlan, closed []string) {
	want := map[string]bool{}
	for _, h := range cands {
		want[h] = true
	}
	best := map[string]HalfPlan{}
	for _, m := range mems {
		if m.Type != "handoff" || strings.Contains(m.ID, archivedMarker) || (m.Scope != "project" && m.Scope != "area") {
			continue
		}
		plan := ExtractPlan(m.Body)
		if plan == "" {
			continue
		}
		hash := PlanHash(plan)
		if !want[hash] {
			continue
		}
		var left []int
		for _, n := range PlanBatches(plan) {
			if !batchDone(files.Verify[hash][n]) {
				left = append(left, n)
			}
		}
		if len(left) == 0 {
			continue
		}
		goal := PlanGoal(plan)
		if goal == "" {
			goal = strings.TrimSpace(strings.TrimPrefix(m.Title, "handoff: "))
		}
		if goal == "" {
			goal = "feature de la 1.x " + hash
		}
		hp := HalfPlan{Hash: hash, Goal: goal, Plan: plan, Open: left, Started: len(files.Verify[hash]) > 0, Updated: m.Updated}
		if old, ok := best[hash]; !ok || hp.Updated > old.Updated {
			best[hash] = hp
		}
	}
	for _, h := range cands {
		if hp, ok := best[h]; ok {
			open = append(open, hp)
		} else {
			closed = append(closed, h)
		}
	}
	sort.SliceStable(open, func(i, j int) bool { return open[i].Updated > open[j].Updated })
	return open, closed
}

func legacyMissing(open []int, one, many string) string {
	if len(open) == 1 {
		return one + domain.JoinInts(open, "y")
	}
	return many + domain.JoinInts(open, "y")
}

// HalfPlanKey is the key of the mark of one plan in one checkout.
func HalfPlanKey(checkout, hash string) string { return checkout + "#" + hash }

// HalfPlanNotice is the line shown at SessionStart.
func HalfPlanNotice(hp HalfPlan) string {
	what := "aprobada, sin lotes corridos"
	if hp.Started {
		what = "faltan los lotes " + domain.JoinInts(hp.Open, "y")
		if len(hp.Open) == 1 {
			what = "falta el lote " + domain.JoinInts(hp.Open, "y")
		}
	}
	return "nxy: quedó a medias una feature de la 1.x en este repo: «" + hp.Goal + "» (" + what + "). " +
		"Te pregunto si la planifico de nuevo con la v2; el plan viejo no se retoma."
}

const halfPlanQuestionHead = "Quedó a medias la feature de la 1.x «"
const halfPlanQuestionMid = "» (plan "
const halfPlanQuestionTail = "). ¿La planifico de nuevo con la v2?"

// HalfPlanQuestion is the text of the AskUserQuestion; it carries the plan hash.
func HalfPlanQuestion(hp HalfPlan) string {
	return halfPlanQuestionHead + hp.Goal + halfPlanQuestionMid + hp.Hash + halfPlanQuestionTail
}

// ParseHalfPlanHash is the plan hash of a question made by HalfPlanQuestion.
func ParseHalfPlanHash(q string) (string, bool) {
	if !strings.HasPrefix(q, halfPlanQuestionHead) || !strings.HasSuffix(q, halfPlanQuestionTail) {
		return "", false
	}
	rest := strings.TrimSuffix(q, halfPlanQuestionTail)
	i := strings.LastIndex(rest, halfPlanQuestionMid)
	if i < 0 {
		return "", false
	}
	h := rest[i+len(halfPlanQuestionMid):]
	if !ValidPlanHash(h) {
		return "", false
	}
	return h, true
}

// Request is the request of the v2 feature that re-plans it: the Goal, then the 1.x plan
// indented as reference (no line of it starts with "#", so it reads as one block).
func (hp HalfPlan) Request() string {
	var b strings.Builder
	b.WriteString(hp.Goal)
	b.WriteString("\n\nFeature de la 1.x que quedó a medias (plan " + hp.Hash)
	if hp.Started {
		b.WriteString(legacyMissing(hp.Open, ", faltaba el lote ", ", faltaban los lotes "))
	}
	b.WriteString("). Planificala de nuevo con la v2; el plan viejo va sólo como referencia:\n\n")
	for i, l := range strings.Split(strings.ReplaceAll(hp.Plan, "\r\n", "\n"), "\n") {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString("    " + l)
	}
	return b.String()
}
