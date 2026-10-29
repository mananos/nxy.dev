package main

import (
	"os"

	"github.com/matyy-a/nxy.dev/internal/app"
	"github.com/matyy-a/nxy.dev/internal/filter"
	"github.com/matyy-a/nxy.dev/internal/host/claudecode"
	"github.com/matyy-a/nxy.dev/internal/platform"
	"github.com/matyy-a/nxy.dev/internal/tools"
)

func homeDir() string {
	h, _ := os.UserHomeDir()
	return h
}

type filterConfig struct{}

func (filterConfig) Filter(projectDir string) filter.Settings {
	return platform.LoadFilterSettings(projectDir, os.Getenv)
}

type permissionRules struct{}

func (permissionRules) Rules(projectDir string) filter.Rules {
	return claudecode.LoadPermissionRules(claudecode.SettingsFiles(homeDir(), projectDir))
}

type filterEngine struct{ e tools.Engine }

func (f filterEngine) Info(s filter.Settings, projectDir string) filter.EngineInfo {
	return f.e.Info(s, func() bool {
		return filter.RtkHookIn(claudecode.PreToolUseHookCommands(claudecode.SettingsFiles(homeDir(), projectDir)))
	})
}

func (filterEngine) Rewrite(rtkPath, cmd string) (filter.RewriteResult, error) {
	return tools.Rewrite(rtkPath, cmd)
}

func newHooks() app.Hooks {
	return app.Hooks{Config: filterConfig{}, Engine: filterEngine{}, Permissions: permissionRules{}}
}
