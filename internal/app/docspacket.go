package app

import (
	"context"
	"strings"

	"github.com/mananos/nxy.dev/internal/docs"
	"github.com/mananos/nxy.dev/internal/plan"
)

// NoDocsText is what `nxy review docs` prints for a feature with no docs to update.
const NoDocsText = "no docs mention what this change touched: nothing to update"

// DocsPacket is what `nxy review docs --feature <id>` prints for the documenter: the plan (or the
// request when there is none), the docs that name the change with their lines, the files that
// changed since the approved base and their diff.
func (h Hooks) DocsPacket(ctx context.Context, id string) (string, error) {
	f, err := h.Store.Feature(ctx, id)
	if err != nil {
		return "", err
	}
	if f.Docs == nil || len(f.Docs.Docs) == 0 {
		return NoDocsText, nil
	}
	text := f.Request
	if f.Plan != nil {
		text = plan.Markdown(f)
	}
	in := docs.PacketIn{ID: f.ID, Plan: text, Docs: f.Docs.Docs, WikiDir: f.Docs.WikiDir}
	for _, d := range h.reviewDiffs(ctx, f) {
		added, removed := countChanged(d.Text)
		in.Files = append(in.Files, docs.File{Path: d.Path, Status: d.Status, Added: added, Removed: removed, Diff: d.Text})
	}
	return docs.Packet(in), nil
}

// countChanged counts the added and removed lines of a unified diff (after its first hunk header).
func countChanged(diff string) (added, removed int) {
	inHunk := false
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "@@"):
			inHunk = true
		case !inHunk:
		case strings.HasPrefix(line, "+"):
			added++
		case strings.HasPrefix(line, "-"):
			removed++
		}
	}
	return added, removed
}
