package app

import (
	"context"
	"path"
	"runtime"
	"strconv"
	"strings"

	"github.com/mananos/nxy.dev/internal/domain"
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
	if ev.FilePath == "" || (ev.AgentID != "" && ev.AgentType != roleImplementer) {
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
	if ev.AgentID != "" {
		if f.State.Phase == domain.PhaseRunningBatch {
			h.noteWrite(ctx, f.ID)
			if ev.AgentType == roleImplementer && h.batchNamesFile(f, ev) {
				return HookResponse{AllowReason: "nxy: batch " + strconv.Itoa(f.State.Batch) + " of feature " + f.ID + " names this file"}
			}
		}
		return HookResponse{}
	}
	return HookResponse{Deny: mainThreadWriteDeny(f)}
}

// batchNamesFile reports whether the edited file is one the current batch's change list
// names (path part of path:line, relative to the project dir) and stays inside it.
func (h Hooks) batchNamesFile(f domain.Feature, ev HookEvent) bool {
	if f.Plan == nil || f.State.Batch < 1 || f.State.Batch > len(f.Plan.Batches) {
		return false
	}
	root := normPath(f.ProjectDir, "")
	target := normPath(ev.FilePath, ev.Cwd)
	if !within(root, target) {
		return false
	}
	for _, c := range f.Plan.Batches[f.State.Batch-1].Change {
		p := changePath(c.At)
		if p == "" {
			continue
		}
		if h.Files != nil {
			p = h.Files.NativePath(p)
		}
		if normPath(p, f.ProjectDir) == target {
			return true
		}
	}
	return false
}

// changePath drops a trailing :line or :line-line from a change location.
func changePath(at string) string {
	at = strings.TrimSpace(at)
	if i := strings.LastIndex(at, ":"); i >= 0 {
		rest := at[i+1:]
		if strings.Trim(rest, "0123456789-,") == "" {
			at = at[:i]
		}
	}
	return at
}

func mainThreadWriteDeny(f domain.Feature) string {
	next := nextSituation(f)
	if f.Next.MainThread() && f.Next.Label() != "" {
		next = "next step " + next
	}
	return "nxy: feature " + f.ID + " is in progress — the main thread does not write code; " +
		next + ". To end the feature: `nxy feature stop`"
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
