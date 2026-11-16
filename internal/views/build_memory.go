package views

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mananos/nxy.dev/internal/memory"
)

// Thresholds of the memory screen, fixed in code (the 10 is an estimate, nobody measures it).
const (
	// UncitedDeliveries is how many times a rule must be delivered, never cited, to be flagged.
	UncitedDeliveries = 10
	// SeenFeatures is how many sightings make a learning worth promoting.
	SeenFeatures = 2
)

// MemoryIn is the data of Memoria for one scope.
type MemoryIn struct {
	Scope     string // global|repo
	Repo      RepoRef
	Rules     []memory.Rule
	Learnings []memory.Learning
	Closed    []memory.ClosedFeature
	// Exists says whether a file is on disk; nil counts every file as there.
	Exists func(path string) bool
}

// BuildMemory maps rules, learnings and closed features to the screen. The odd items come first
// in each list and the proposals are derived from them.
func BuildMemory(in MemoryIn) Memory {
	global := in.Scope == ScopeGlobal
	m := Memory{
		Scope:     in.Scope,
		Repo:      in.Repo,
		Rules:     []Rule{},
		Learnings: []Learning{},
		Closed:    []ClosedFeature{},
		Proposals: []Proposal{},
	}
	if global && m.Repo.Name == "" {
		m.Repo.Name = ScopeGlobal
	}
	m.Create = Action{ID: "create", Label: "Nueva regla", Argv: []string{"nxy", "mem", "rule", "{text}", "--question", "{question}", "--paths", "{paths}"}}
	if global {
		m.Create.Label = "Nueva regla tuya"
		m.Create.Argv = append(m.Create.Argv, "--global", "--roles", "{roles}")
		m.Create.Confirm = "Crea una regla tuya que vale en todos los repos"
	}

	for _, r := range in.Rules {
		m.Rules = append(m.Rules, ruleOf(r, global))
	}
	sort.SliceStable(m.Rules, func(i, j int) bool { return m.Rules[i].Flag != "" && m.Rules[j].Flag == "" })

	for _, l := range in.Learnings {
		m.Learnings = append(m.Learnings, learningOf(l, in))
	}
	sort.SliceStable(m.Learnings, func(i, j int) bool { return m.Learnings[i].Flag != "" && m.Learnings[j].Flag == "" })

	kept := map[string]bool{} // "Dejarla": nxy does not propose these again
	for _, r := range in.Rules {
		if r.KeptAt != 0 {
			kept[ruleOf(r, global).ID] = true
		}
	}
	for _, l := range in.Learnings {
		if l.KeptAt != 0 {
			kept[learningOf(l, in).ID] = true
		}
	}

	for _, r := range m.Rules {
		if r.Flag != FlagUncited || kept[r.ID] {
			continue
		}
		m.Proposals = append(m.Proposals, Proposal{
			ID: "delete:" + r.ID, Kind: "delete", Target: r.ID, Text: r.Text,
			Why:    "La recibieron " + strconv.Itoa(r.Delivered) + " veces y nadie la citó",
			Accept: Action{ID: "accept", Label: "Borrarla", Argv: []string{"nxy", "mem", "rm", r.ID}, Confirm: "Borra la regla " + r.ID},
			Reject: keepAction(r.ID),
		})
	}
	// The input holds only live, non-promoted, non-obsolete learnings (store.Learnings).
	for _, l := range m.Learnings {
		if kept[l.ID] {
			continue
		}
		var why string
		switch {
		case l.Flag == FlagSeen:
			why = "Se vio " + strconv.Itoa(l.Seen) + " veces: ya parece una regla"
		case isGlobalLearning(in.Learnings, l.ID):
			why = "Viene de la 1.x como aprendizaje global: conviene que sea una regla tuya"
		default:
			continue
		}
		m.Proposals = append(m.Proposals, Proposal{
			ID: "promote:" + l.ID, Kind: "promote", Target: l.ID, Text: l.Text, Why: why,
			Accept: Action{ID: "accept", Label: "Hacerla regla", Argv: []string{"nxy", "mem", "promote", l.ID, "--question", "{question}"}, Confirm: "Convierte " + l.ID + " en una regla"},
			Reject: keepAction(l.ID),
		})
	}

	for _, c := range in.Closed {
		cf := ClosedFeature{ID: c.ID, Title: c.Title, ClosedAt: rfc(c.CreatedAt), Outcome: "success", Files: []string{}}
		for _, f := range c.Files {
			cf.Files = append(cf.Files, f.Path)
		}
		m.Closed = append(m.Closed, cf)
	}
	return m
}

// restoreAction undoes a delete (the TUI's `u` a few seconds later).
func restoreAction(id string) Action {
	return Action{ID: "restore", Label: "Deshacer", Argv: []string{"nxy", "mem", "restore", id}}
}

func keepAction(id string) Action {
	return Action{ID: "reject", Label: "Dejarla", Argv: []string{"nxy", "mem", "keep", id}}
}

func isGlobalLearning(ls []memory.Learning, short string) bool {
	for _, l := range ls {
		if l.Short == short {
			return l.Repo == memory.GlobalRepo
		}
	}
	return false
}

func rfc(ms int64) string {
	if ms <= 0 {
		return ""
	}
	return time.UnixMilli(ms).UTC().Format(time.RFC3339)
}

func originOf(o string) string {
	switch o {
	case memory.OriginPlan, memory.OriginEngineer, memory.OriginReview:
		return "closing"
	case memory.OriginChat, memory.OriginManual:
		return "chat"
	}
	return o
}

func ruleOf(r memory.Rule, global bool) Rule {
	id := r.Short
	if id == "" {
		id = r.ID
	}
	out := Rule{
		ID: id, Text: r.Text, Question: r.Question, Paths: nonNilStrings(r.Paths),
		Origin: originOf(r.Origin), CreatedAt: rfc(r.CreatedAt), UpdatedAt: rfc(r.UpdatedAt),
		Cited: r.Citations, LastCited: rfc(r.LastCitedAt), Delivered: r.Deliveries,
		Roles: nonNilStrings(r.Roles),
	}
	if out.Origin == "closing" {
		out.OriginRef = r.FeatureID
	}
	if r.Deliveries >= UncitedDeliveries && r.Citations == 0 {
		out.Flag = FlagUncited
	}
	edit := []string{"nxy", "mem", "edit", id, "--text", "{text}", "--question", "{question}", "--paths", "{paths}"}
	confirm := "Cambia " + id
	if global {
		edit = append(edit, "--roles", "{roles}")
		confirm = "Cambia " + id + " y a qué roles llega"
	}
	out.Actions = []Action{
		{ID: "edit", Label: "Editar", Argv: edit, Confirm: confirm},
		{ID: "delete", Label: "Borrar", Argv: []string{"nxy", "mem", "rm", id}, Confirm: "Borra la regla " + id},
		restoreAction(id),
	}
	return out
}

func learningOf(l memory.Learning, in MemoryIn) Learning {
	id := l.Short
	if id == "" {
		id = l.ID
	}
	out := Learning{
		ID: id, Text: l.Text, Files: nonNilStrings(l.Files), Seen: l.Seen, Features: []string{},
		Stale: learningStale(l, in),
	}
	if l.FeatureID != "" {
		out.Features = []string{l.FeatureID}
	}
	switch {
	case out.Stale:
		out.Flag = FlagStale
	case l.Seen >= SeenFeatures:
		out.Flag = FlagSeen
	}
	out.Actions = []Action{
		{ID: "edit", Label: "Editar", Argv: []string{"nxy", "mem", "edit", id, "--text", "{text}", "--files", "{files}"}, Confirm: "Cambia " + id},
		{ID: "delete", Label: "Borrar", Argv: []string{"nxy", "mem", "rm", id}, Confirm: "Borra el aprendizaje " + id},
		restoreAction(id),
	}
	return out
}

// learningStale: it has files and none is left on disk. Obsolete learnings never get here
// (the provider lists only live ones); without a repo path there is nothing to stat.
func learningStale(l memory.Learning, in MemoryIn) bool {
	if in.Exists == nil || len(l.Files) == 0 || in.Repo.Path == "" {
		return false
	}
	for _, f := range l.Files {
		p := f
		if !filepath.IsAbs(f) && !strings.HasPrefix(f, "/") {
			p = filepath.Join(in.Repo.Path, f)
		}
		if in.Exists(p) {
			return false
		}
	}
	return true
}
