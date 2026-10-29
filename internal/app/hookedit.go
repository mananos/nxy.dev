package app

import (
	"context"
	"path"
	"runtime"
	"strings"
)

// goos is the OS the path comparison follows; a variable so tests can pin it.
var goos = runtime.GOOS

func isEditTool(tool string) bool {
	switch tool {
	case "Edit", "Write", "NotebookEdit", "MultiEdit":
		return true
	}
	return false
}

// preToolEdit refuses edits inside the repo from the main thread while a feature is active.
func (h Hooks) preToolEdit(ctx context.Context, ev HookEvent) HookResponse {
	if ev.AgentID != "" || ev.FilePath == "" {
		return HookResponse{}
	}
	f, ok := h.activeFeature(ctx, ev)
	if !ok {
		return HookResponse{}
	}
	root := f.ProjectDir
	if root == "" {
		return HookResponse{}
	}
	if !within(normPath(root, ""), normPath(ev.FilePath, ev.Cwd)) {
		return HookResponse{}
	}
	return HookResponse{Deny: "nxy: feature " + f.ID + " is in progress — the main thread does not write code; next step " +
		f.Next.Label() + ". To end the feature: `nxy feature stop`"}
}

// normPath makes p absolute against cwd, with forward slashes, cleaned and (on Windows)
// lowercase. Git Bash and extended prefixes are already resolved by the host decoder
// (platform.NativePath).
func normPath(p, cwd string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	if !isAbs(p) && cwd != "" {
		p = strings.ReplaceAll(cwd, "\\", "/") + "/" + p
	}
	p = path.Clean(p)
	if goos == "windows" {
		p = strings.ToLower(p)
	}
	return p
}

func isLetter(b byte) bool { return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') }

func isAbs(p string) bool {
	return strings.HasPrefix(p, "/") || (len(p) >= 2 && p[1] == ':' && isLetter(p[0]))
}

func within(root, p string) bool {
	return p == root || strings.HasPrefix(p, strings.TrimSuffix(root, "/")+"/")
}
