package main

import (
	"os"
	"path/filepath"

	"github.com/mananos/nxy.dev/v2/internal/platform"
)

// resolveCwd is the working directory every utility command uses: `--cwd <dir>` if given, else
// CLAUDE_PROJECT_DIR, else the process directory, in its native spelling.
func resolveCwd(a setupArgs) string {
	cwd := a.cwd
	if cwd == "" {
		cwd = os.Getenv("CLAUDE_PROJECT_DIR")
	}
	if cwd == "" {
		cwd = osGetwd()
	}
	return platform.NativePath(cwd)
}

// splitRepo takes `--repo` out of the positional words.
func splitRepo(positional []string) (repo bool, pos []string) {
	for _, p := range positional {
		if p == "--repo" {
			repo = true
		} else {
			pos = append(pos, p)
		}
	}
	return repo, pos
}

// configFilePaths is the user config (~/.nxy/config.json) and the repo one (<git root>/.nxy/config.json).
func configFilePaths(cwd string) (user, repo string, err error) {
	u, err := platform.UserDirEnv(os.Getenv)
	if err != nil {
		return "", "", err
	}
	return filepath.Join(u, "config.json"), filterConfigPath(cwd), nil
}
