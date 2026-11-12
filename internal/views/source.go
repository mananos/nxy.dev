package views

import "context"

// Source gives each view built from the real data of the current repo. cmd/nxy implements it (it
// reads the db, the transcripts, the config and the tools) and hands it to the TUI.
type Source interface {
	Cockpit(ctx context.Context) (Cockpit, error)
	Agents(ctx context.Context, featureID string) (Agents, error)
	// Decision is false when the feature has no question waiting.
	Decision(ctx context.Context, featureID string) (Decision, bool, error)
	// Memory is the scope "repo" (the repo of the folder) or "global".
	Memory(ctx context.Context, scope string) (Memory, error)
	// Stats is the session with that id ("" is the current one, else the last of the repo).
	Stats(ctx context.Context, session string) (Stats, error)
	Trend(ctx context.Context) (Trend, error)
	Config(ctx context.Context) (Config, error)
	// Plan is false when the feature has no plan yet.
	Plan(ctx context.Context, featureID string) (Plan, bool, error)
	// File is one file of the feature (path as written in the plan).
	File(ctx context.Context, featureID, path string) (File, error)
	// Engineer is false when the feature has no design.
	Engineer(ctx context.Context, featureID string) (Engineer, bool, error)
}
