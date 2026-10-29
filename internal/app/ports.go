package app

import (
	"context"
	"time"

	"github.com/matyy-a/nxy.dev/internal/domain"
	"github.com/matyy-a/nxy.dev/internal/filter"
)

// Store persists features. There is no Load/Save: changes go through Transition.
type Store interface {
	CreateFeature(ctx context.Context, f domain.Feature) (string, error)
	// Transition applies fn to the stored feature. fn must be pure: it can run more than once.
	Transition(ctx context.Context, id string, fn func(domain.Feature) (domain.Feature, error)) error
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
	Rewrite(rtkPath, cmd string) (filter.RewriteResult, error)
}

// PermissionRules gives the host's allow/deny rules for a project.
type PermissionRules interface {
	Rules(projectDir string) filter.Rules
}
