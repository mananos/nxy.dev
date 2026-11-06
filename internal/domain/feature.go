package domain

import "strings"

// Feature is one user request moving through the flow. It is stored as JSON so
// later phases add fields without a migration.
type Feature struct {
	ID         string   `json:"id"`
	Repo       string   `json:"repo"`
	ProjectDir string   `json:"project_dir"`
	Request    string   `json:"request"` // the user's words, verbatim
	State      State    `json:"state"`
	Next       Action   `json:"next"`
	Plan       *Plan    `json:"plan,omitempty"`
	PlanRev    int      `json:"plan_rev"`
	Answers    []Answer `json:"answers,omitempty"`
	// ChangeNotes are the user's literal notes asking to change the plan.
	ChangeNotes []string `json:"change_notes,omitempty"`
	// Proof is the evidence of the batch being implemented.
	Proof *Proof `json:"proof,omitempty"`
	// Base is the HEAD sha at approval; the changed files are counted from it.
	Base string `json:"base,omitempty"`
	// Affected is the last run of the affected tests.
	Affected *AffectedRun `json:"affected,omitempty"`
	// Review is the state of the review of the finished feature.
	Review *Review `json:"review,omitempty"`
	// Background is the role of the nxy agent launched in the background whose end has not been handled.
	Background string `json:"background,omitempty"`
	// Undelivered is true while Next still has to reach the main thread.
	Undelivered bool `json:"undelivered,omitempty"`
	// Mode is who decides the design: the planner (delegate) or the user (engineer).
	Mode Mode `json:"mode,omitempty"`
	// Design is the design debate of an engineer feature.
	Design *Design `json:"design,omitempty"`
	// Docs are the docs of the repo that name what the feature changed, found when it closes.
	Docs *Docs `json:"docs,omitempty"`
	// OfferAnswered is true once the user answered the "Pasar a engineer" offer.
	OfferAnswered bool `json:"offer_answered,omitempty"`
	// Harvested are the ids of the implementer agents whose learnings were already saved.
	Harvested []string `json:"harvested,omitempty"`
	// Candidates are the rule candidates raised while the feature runs (review escapes, chat);
	// RuleOffer is what the closing question offered (S1..).
	Candidates []RuleCandidate `json:"candidates,omitempty"`
	// Delivered says, for each K/A id the last implementer received, the repos (by name) it came from:
	// with 2+ repos the same id can be delivered from two of them.
	Delivered map[string][]string `json:"delivered,omitempty"`
	RuleOffer []RuleCandidate     `json:"rule_offer,omitempty"`
	// Repos are the repos of a multi-repo plan, set when the plan is saved; empty for a one-repo feature.
	Repos []FeatureRepo `json:"repos,omitempty"`
	// Home is the checkout key of the folder the feature was started from; empty before the migration.
	Home string `json:"home,omitempty"`
	// Holds are the checkouts the feature holds (one active feature per checkout).
	Holds []string `json:"holds,omitempty"`
	// Paused is true while the feature is paused: it holds nothing.
	Paused bool `json:"paused,omitempty"`
	// InWorktree is true when the only repo goes to a worktree because its checkout was busy.
	InWorktree bool `json:"in_worktree,omitempty"`
	// Clash lists the busy checkouts that stopped the plan from reaching approval.
	Clash []RepoClash `json:"clash,omitempty"`
	// PendingPlan is the plan waiting for the clash to be solved.
	PendingPlan *Plan `json:"pending_plan,omitempty"`
	// BranchOffer is what the branch question of the approved plan offers and has collected so far.
	BranchOffer *BranchOffer `json:"branch_offer,omitempty"`
}

// BranchOffer is the branch question of a feature: the names suggested, the bases per repo and the
// answers already given (a missing one is asked again). Error is what git said to the last try.
type BranchOffer struct {
	Suggest []string          `json:"suggest,omitempty"`
	Branch  string            `json:"branch,omitempty"`
	Repos   []BranchRepoOffer `json:"repos,omitempty"`
	Error   string            `json:"error,omitempty"`
}

// BranchRepoOffer is the bases offered for one repo; Base is the one the user picked.
type BranchRepoOffer struct {
	Name    string   `json:"name"`
	Current string   `json:"current,omitempty"`
	Bases   []string `json:"bases,omitempty"` // the default first
	Base    string   `json:"base,omitempty"`
}

// FeatureRepo is one repo of a feature with its checkout and branch.
type FeatureRepo struct {
	Name       string `json:"name"`
	Key        string `json:"key"`
	Root       string `json:"root"`
	Checkout   string `json:"checkout,omitempty"`
	Branch     string `json:"branch,omitempty"`
	Base       string `json:"base,omitempty"`
	Role       string `json:"role,omitempty"`
	Undeclared bool   `json:"undeclared,omitempty"`
}

// RepoClash is a checkout the feature needs that another feature holds.
type RepoClash struct {
	Repo      string `json:"repo"`
	Checkout  string `json:"checkout"`
	FeatureID string `json:"feature_id"`
	Phase     string `json:"phase,omitempty"`
}

// RepoList is the repos of the feature. Without Repos it is one implicit repo built from
// ProjectDir, Base and Repo, so old and one-repo features keep working.
func (f Feature) RepoList() []FeatureRepo {
	if len(f.Repos) > 0 {
		return f.Repos
	}
	return []FeatureRepo{{Key: f.Repo, Root: f.ProjectDir, Checkout: f.ProjectDir, Base: f.Base}}
}

// RepoByName is the repo called name.
func (f Feature) RepoByName(name string) (FeatureRepo, bool) {
	for _, r := range f.Repos {
		if r.Name == name {
			return r, true
		}
	}
	return FeatureRepo{}, false
}

// UsesWorktrees is true when the feature works in worktrees: the branch step created them (2+ repos
// or a busy checkout) and set InWorktree. A repo added later as undeclared does not change it.
func (f Feature) UsesWorktrees() bool {
	return f.InWorktree
}

// WantsWorktrees is what the branch step decides at approval: 2+ declared repos, or a busy checkout.
func (f Feature) WantsWorktrees() bool {
	return len(f.Repos) > 1 || f.InWorktree
}

// Held is the checkouts the feature holds: none when done or paused; Holds; and, for a feature
// from before the migration (no Home), its repo key.
func (f Feature) Held() []string {
	if f.State.Phase == PhaseDone || f.Paused {
		return nil
	}
	if len(f.Holds) > 0 {
		return f.Holds
	}
	if f.Home == "" && f.Repo != "" {
		return []string{f.Repo}
	}
	return nil
}

// RepoTable tells an agent where each repo of a multi-repo plan lives: the paths of the plan start
// with the repo name, and "<name>/x" is "<checkout>/x". Empty when the plan declares no repos.
func (f Feature) RepoTable() string {
	if f.Plan == nil || len(f.Plan.Repos) == 0 || len(f.Repos) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n## Repos\n\nThe paths of the plan start with the repo name: `<name>/x` is `<checkout>/x`. Edit only inside the checkout.\n")
	for _, r := range f.Repos {
		dir := r.Checkout
		if dir == "" {
			dir = r.Root
		}
		b.WriteString("\n- " + r.Name + " — checkout `" + strings.ReplaceAll(dir, `\`, "/") + "`")
		if r.Role != "" {
			b.WriteString(" (" + r.Role + ")")
		}
	}
	b.WriteString("\n")
	return b.String()
}

// RuleCandidate is a convention that may become a rule of the repo when the feature closes.
type RuleCandidate struct {
	ID             string `json:"id,omitempty"` // "S1", set when it is offered
	Text           string `json:"text"`
	Question       string `json:"question"`
	Origin         string `json:"origin"`
	Lens           string `json:"lens,omitempty"`
	Repo           string `json:"repo,omitempty"`            // name of the repo it belongs to (multi-repo plans); empty is the first
	Supersedes     string `json:"supersedes,omitempty"`      // K<n> of the rule it would replace
	SupersedesText string `json:"supersedes_text,omitempty"` // that rule's text, for the question
}

// Mode says how a feature is run.
type Mode string

const (
	ModeDelegate Mode = ""
	ModeEngineer Mode = "engineer"
)
