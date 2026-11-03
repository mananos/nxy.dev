package memory

import "errors"

// ErrNotFound and ErrAmbiguous are returned when a reference (K3, A7, N2, a
// ULID or a ULID prefix) matches no row or more than one.
var (
	ErrNotFound  = errors.New("memory: not found")
	ErrAmbiguous = errors.New("memory: ambiguous reference")
)

// AlreadyRuleError is returned when a learning that was already promoted is noted again.
type AlreadyRuleError struct{ Learning, Rule string }

func (e *AlreadyRuleError) Error() string {
	return e.Learning + " is already rule " + e.Rule + ": nothing saved"
}

// Origins of a rule.
const (
	OriginBootstrap = "bootstrap"
	OriginPlan      = "plan"
	OriginEngineer  = "engineer"
	OriginReview    = "review"
	OriginChat      = "chat"
	OriginManual    = "manual"
)

// Origins lists the valid origins.
var Origins = []string{OriginBootstrap, OriginPlan, OriginEngineer, OriginReview, OriginChat, OriginManual}

// Meta is common to every row. Times are Unix milliseconds; DeletedAt != 0
// marks a tombstone (kept so a deletion travels with the export).
type Meta struct {
	ID        string `json:"id"` // ULID: the identity of the row
	Machine   string `json:"machine,omitempty"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
	DeletedAt int64  `json:"deleted_at,omitempty"`
}

// Deleted reports whether the row is a tombstone.
func (m Meta) Deleted() bool { return m.DeletedAt != 0 }

// Rule is a convention of a repo, with the question it settles.
type Rule struct {
	Meta
	Short       string   `json:"short,omitempty"` // "K<n>", a screen alias
	Repo        string   `json:"repo"`
	Text        string   `json:"text"`
	Question    string   `json:"question"`
	Origin      string   `json:"origin"`
	FeatureID   string   `json:"feature_id,omitempty"`
	Paths       []string `json:"paths,omitempty"` // globs
	Supersedes  string   `json:"supersedes,omitempty"`
	Deliveries  int      `json:"deliveries,omitempty"`
	Citations   int      `json:"citations,omitempty"`
	LastCitedAt int64    `json:"last_cited_at,omitempty"`
}

// FileRef is a file of a feature, in a repo.
type FileRef struct {
	Repo string `json:"repo"`
	Path string `json:"path"`
}

// ClosedFeature is what is kept of a closed feature. The minimum is Title and
// Files; a feature may span several repos.
type ClosedFeature struct {
	Meta
	Title     string    `json:"title"`
	Repos     []string  `json:"repos,omitempty"`
	Decisions []string  `json:"decisions,omitempty"`
	Files     []FileRef `json:"files,omitempty"`
	Findings  []string  `json:"findings,omitempty"`
}

// Learning is something learnt that is not yet a rule.
type Learning struct {
	Meta
	Short      string   `json:"short,omitempty"` // "A<n>"
	Repo       string   `json:"repo"`
	Text       string   `json:"text"`
	Files      []string `json:"files,omitempty"`
	FeatureID  string   `json:"feature_id,omitempty"`
	Hash       string   `json:"hash"`
	Seen       int      `json:"seen"`
	LastSeenAt int64    `json:"last_seen_at,omitempty"`
	ObsoleteAt int64    `json:"obsolete_at,omitempty"`
	PromotedTo string   `json:"promoted_to,omitempty"` // ULID of the rule
}

// Hit is a search result.
type Hit struct {
	Kind  string // "rule", "learning" or "feature"
	ID    string
	Short string // K<n> / A<n>; empty for features
	Repo  string
	Text  string
}

// Note is a temporary note, tied to a folder.
type Note struct {
	Meta
	Short     string `json:"short,omitempty"` // "N<n>"
	Folder    string `json:"folder"`
	Title     string `json:"title"`
	Body      string `json:"body,omitempty"`
	Session   string `json:"session,omitempty"`
	FeatureID string `json:"feature_id,omitempty"`
}
