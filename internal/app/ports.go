package app

import (
	"context"
	"errors"
	"time"

	"github.com/mananos/nxy.dev/internal/domain"
	"github.com/mananos/nxy.dev/internal/filter"
)

// ErrActiveFeature is what CreateFeature returns when the repo already has a feature that is not done.
var ErrActiveFeature = errors.New("repo already has an active feature")

// Store persists features. There is no Load/Save: changes go through Transition.
type Store interface {
	CreateFeature(ctx context.Context, f domain.Feature) (string, error)
	// Feature reads one feature; it fails when the id is unknown.
	Feature(ctx context.Context, id string) (domain.Feature, error)
	// ActiveFeature is the most recent feature of the repo that is not done.
	ActiveFeature(ctx context.Context, repo string) (domain.Feature, bool, error)
	// Transition applies fn to the stored feature. fn must be pure: it can run more than once.
	Transition(ctx context.Context, id string, fn func(domain.Feature) (domain.Feature, error)) error
}

// Repos gives the stable key of the repository a directory belongs to.
type Repos interface {
	Key(dir string) string
}

// Legacy tells whether the 1.x plugin is still enabled for a project.
type Legacy interface {
	Active(projectDir string) bool
}

// Viewer shows the full plan to the user and cleans it up afterwards.
type Viewer interface {
	Show(ctx context.Context, f domain.Feature) (string, error)
	Discard(id string) error
}

// Clock gives the current time.
type Clock interface {
	Now() time.Time
}

// FilterConfig gives the filter settings for a project.
type FilterConfig interface {
	Filter(projectDir string) filter.Settings
}

// FilterEngine resolves which tool rewrites commands and asks it to.
type FilterEngine interface {
	Info(s filter.Settings, projectDir string) filter.EngineInfo
	Rewrite(info filter.EngineInfo, cmd string) (filter.RewriteResult, error)
}

// ShellResult is the outcome of a command run by Shell: Output is the tail of stdout and stderr.
type ShellResult struct {
	Exit   int
	Output string
	// TimedOut: the command ran out of time and was killed (Exit is -1).
	TimedOut bool
}

// Shell runs a command line with the system shell. It fails only when the shell cannot
// start or ctx expires; a command that exits non-zero is a result, not an error.
type Shell interface {
	Run(ctx context.Context, dir, command string) (ShellResult, error)
}

// Files answers the two questions the Bash write parser asks of the file system.
type Files interface {
	// NativePath turns a Git Bash path (/c/x) into the native one.
	NativePath(p string) string
	IsDir(path string) bool
}

// PermissionRules gives the host's allow/deny rules for a project.
type PermissionRules interface {
	Rules(projectDir string) filter.Rules
}
