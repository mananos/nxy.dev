package domain

// Feature is a placeholder; it is stored as JSON so later phases add fields without a migration.
type Feature struct {
	ID    string `json:"id"`
	Phase string `json:"phase"`
}
