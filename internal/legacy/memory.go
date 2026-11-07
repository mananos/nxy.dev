package legacy

import (
	"sort"
	"strings"

	"github.com/mananos/nxy.dev/internal/memory"
)

const (
	filePrefix     = "file:"
	handoffPrefix  = "handoff: "
	featurePrefix  = "feature: "
	archivedMarker = "-done-"
)

// ConvertMemory turns the memories of one project of the 1.x (already filtered
// by its project key) and their edges into what the v2 keeps for repo, with the
// live handoffs as notes of folder. now (Unix ms) dates the expiry of notes.
//
// Conventions, decisions, bugs and preferences become learnings; a live handoff
// a note; an archived one (id with "-done-") a closed feature. File edges become
// the files of the learning or feature; a memory another one supersedes is not
// imported; the other edges are dropped and counted. Global memories are counted
// and left out.
func ConvertMemory(mems []Memory, edges []Edge, repo, folder string, now int64) Import {
	var imp Import
	c := &imp.Counts

	files := map[string][]string{}
	superseded := map[string]bool{}
	for _, e := range edges {
		switch e.Kind {
		case "file":
			if p := strings.TrimPrefix(e.Dst, filePrefix); p != "" {
				files[e.Src] = append(files[e.Src], strings.ReplaceAll(p, `\`, "/"))
			}
		case "supersedes":
			superseded[e.Dst] = true
		default:
			c.EdgesDropped++
		}
	}

	sorted := append([]Memory(nil), mems...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Created < sorted[j].Created })

	ttl := memory.NoteTTL.Milliseconds()
	for _, m := range sorted {
		if m.Scope == "global" {
			c.Globals++
			continue
		}
		if m.Scope != "project" && m.Scope != "area" {
			continue
		}
		if superseded[m.ID] {
			c.Superseded++
			continue
		}
		meta := memory.Meta{CreatedAt: m.Created, UpdatedAt: m.Updated}
		switch m.Type {
		case "convention", "decision", "bug", "preference":
			if m.Private {
				c.Private++
				continue
			}
			switch m.Type {
			case "convention":
				c.Conventions++
			case "decision":
				c.Decisions++
			case "bug":
				c.Bugs++
			default:
				c.Preferences++
			}
			text := strings.TrimSpace(m.Title)
			if m.Area != "" {
				text += " (área " + m.Area + ")"
			}
			fs := uniq(files[m.ID])
			imp.Sources = append(imp.Sources, Source{Kind: SourceLearning, Index: len(imp.Learnings), LegacyID: m.ID})
			imp.Learnings = append(imp.Learnings, memory.Learning{
				Meta: meta, Repo: repo, Text: text, Files: fs,
				Hash: memory.LearningHash(text, fs), Seen: 1, LastSeenAt: m.Updated,
			})
			c.Learnings++
		case "handoff":
			if strings.Contains(m.ID, archivedMarker) {
				if m.Private {
					c.Private++
					continue
				}
				var refs []memory.FileRef
				for _, f := range uniq(files[m.ID]) {
					refs = append(refs, memory.FileRef{Repo: repo, Path: f})
				}
				imp.Sources = append(imp.Sources, Source{Kind: SourceFeature, Index: len(imp.Features), LegacyID: m.ID})
				imp.Features = append(imp.Features, memory.ClosedFeature{
					Meta: meta, Title: strings.TrimPrefix(m.Title, featurePrefix), Repos: []string{repo}, Files: refs,
				})
				c.Features++
				continue
			}
			if now-m.Updated > ttl {
				c.NotesExpired++
				continue
			}
			body := NoteBody(m.Body)
			if strings.HasSuffix(body, CutMark) {
				c.NotesCut++
			}
			imp.Sources = append(imp.Sources, Source{Kind: SourceNote, Index: len(imp.Notes), LegacyID: m.ID})
			imp.Notes = append(imp.Notes, memory.Note{
				Meta: meta, Folder: folder, Title: strings.TrimPrefix(m.Title, handoffPrefix), Body: body,
			})
			c.Notes++
		default:
			c.Other++
		}
	}
	return imp
}

func uniq(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
