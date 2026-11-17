package app

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/mananos/nxy.dev/internal/memory"
)

// maxImportBytes caps the bundle an import reads.
const maxImportBytes = 64 << 20

// Memory is the use cases of the memory: rules, learnings, related work, export/import and
// temporary notes. The repo of a directory is keyed like features are (Repos.Key).
type Memory struct {
	Store MemoryStore
	Repos Repos
	Clock Clock
	// FoldCase compares note folders ignoring case (Windows).
	FoldCase bool
}

func (m Memory) now() time.Time {
	if m.Clock != nil {
		return m.Clock.Now()
	}
	return time.Now()
}

func (m Memory) repo(cwd string) string {
	if m.Repos == nil {
		return cwd
	}
	return m.Repos.Key(cwd)
}

// AddRule stores a rule of the repo of cwd. An empty origin is manual. supersedes may be K3 or a ULID.
func (m Memory) AddRule(ctx context.Context, cwd, text, question, origin string, paths []string, feature, supersedes string) (memory.Rule, error) {
	if origin == "" {
		origin = memory.OriginManual
	}
	return m.Store.AddRule(ctx, memory.Rule{
		Repo: m.repo(cwd), Text: strings.TrimSpace(text), Question: strings.TrimSpace(question), Origin: origin,
		Paths: paths, FeatureID: feature, Supersedes: supersedes,
	})
}

// AddGlobalRule stores a global rule (G<n>); no roles means every role. supersedes may be G2 or a ULID.
func (m Memory) AddGlobalRule(ctx context.Context, text, question, origin string, paths, roles []string, supersedes string) (memory.Rule, error) {
	if origin == "" {
		origin = memory.OriginManual
	}
	return m.Store.AddGlobalRule(ctx, memory.Rule{
		Repo: memory.GlobalRepo, Text: strings.TrimSpace(text), Question: strings.TrimSpace(question), Origin: origin,
		Paths: paths, Roles: roles, Supersedes: supersedes,
	})
}

// EditRule changes a rule (K3 or G2) in place: same number, same counters.
func (m Memory) EditRule(ctx context.Context, ref string, e memory.RuleEdit) (memory.Rule, error) {
	return m.Store.EditRule(ctx, ref, e)
}

// IsLearning reports whether a live reference (alias, ULID or prefix) is a learning rather than a rule.
func (m Memory) IsLearning(ctx context.Context, ref string) (bool, error) {
	kind, err := m.Store.MemoryKind(ctx, ref)
	return kind == "learning", err
}

// EditLearning changes a learning (A7) in place.
func (m Memory) EditLearning(ctx context.Context, ref string, e memory.LearningEdit) (memory.Learning, error) {
	return m.Store.EditLearning(ctx, ref, e)
}

// Remove soft-deletes a rule or a learning and returns its short; Restore brings it back.
func (m Memory) Remove(ctx context.Context, ref string) (string, error) {
	return m.Store.DeleteMemory(ctx, ref)
}

// Keep marks a rule or learning as kept: nxy no longer proposes to delete or promote it.
func (m Memory) Keep(ctx context.Context, ref string) (string, error) {
	return m.Store.KeepMemory(ctx, ref)
}

// Restore undoes Remove.
func (m Memory) Restore(ctx context.Context, ref string) (string, error) {
	return m.Store.RestoreMemory(ctx, ref)
}

// ShowGlobal returns the global rules and the global learnings.
func (m Memory) ShowGlobal(ctx context.Context) ([]memory.Rule, []memory.Learning, error) {
	rules, err := m.Store.GlobalRules(ctx)
	if err != nil {
		return nil, nil, err
	}
	learnings, err := m.Store.Learnings(ctx, memory.GlobalRepo)
	if err != nil {
		return nil, nil, err
	}
	return rules, learnings, nil
}

// AddLearning records a learning of the repo; the same one again counts as seen once more.
func (m Memory) AddLearning(ctx context.Context, cwd, text string, files []string) (memory.Learning, error) {
	return m.Store.AddLearning(ctx, memory.Learning{Repo: m.repo(cwd), Text: strings.TrimSpace(text), Files: files})
}

// Show returns the rules and the open learnings of the repo.
func (m Memory) Show(ctx context.Context, cwd string) ([]memory.Rule, []memory.Learning, error) {
	repo := m.repo(cwd)
	rules, err := m.Store.Rules(ctx, repo)
	if err != nil {
		return nil, nil, err
	}
	learnings, err := m.Store.Learnings(ctx, repo)
	if err != nil {
		return nil, nil, err
	}
	return rules, learnings, nil
}

// Search looks text up in the repo's memory (all: in every repo).
func (m Memory) Search(ctx context.Context, cwd, text string, all bool) ([]memory.Hit, error) {
	if strings.TrimSpace(text) == "" {
		return nil, errors.New("no hay nada para buscar")
	}
	return m.Store.Search(ctx, m.repo(cwd), all, text, memory.SearchLimit)
}

// Promote turns a learning into a rule; the question it settles is required.
func (m Memory) Promote(ctx context.Context, ref, question string, paths []string) (memory.Rule, error) {
	return m.Store.Promote(ctx, ref, strings.TrimSpace(question), paths)
}

// Related finds the closed features and learnings that touch the files (relative to the repo root).
func (m Memory) Related(ctx context.Context, cwd string, paths []string) ([]memory.ClosedFeature, []memory.Learning, error) {
	return m.Store.Related(ctx, m.repo(cwd), paths)
}

// Export builds the bundle; temporary notes only travel when includeNotes is set.
func (m Memory) Export(ctx context.Context, includeNotes bool) (memory.Bundle, error) {
	return m.Store.ExportBundle(ctx, includeNotes)
}

// ImportPreview decodes a bundle and says what importing it would do. It writes nothing.
func (m Memory) ImportPreview(ctx context.Context, r io.Reader) (memory.Diff, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxImportBytes+1))
	if err != nil {
		return memory.Diff{}, err
	}
	if len(data) > maxImportBytes {
		return memory.Diff{}, errors.New("el archivo de exportación es demasiado grande")
	}
	b, err := memory.DecodeBundle(data)
	if err != nil {
		return memory.Diff{}, err
	}
	snap, err := m.Store.MemorySnapshot(ctx)
	if err != nil {
		return memory.Diff{}, err
	}
	return memory.Merge(snap, b), nil
}

// ImportApply writes the diff a preview returned, in one transaction. An empty diff writes nothing.
func (m Memory) ImportApply(ctx context.Context, d memory.Diff) error {
	if d.Writes() == 0 {
		return nil
	}
	return m.Store.ApplyImport(ctx, d)
}

// NoteSave stores a temporary note in cwd's folder; with id (N2 or ULID) it updates that note.
func (m Memory) NoteSave(ctx context.Context, cwd, title, body, id, feature, session string) (memory.Note, error) {
	return m.Store.SaveNote(ctx, memory.Note{
		Meta: memory.Meta{ID: id}, Folder: cwd, Title: strings.TrimSpace(title), Body: body, FeatureID: feature, Session: session,
	})
}

// NoteList lists the live notes of cwd's folder (all: of every folder).
func (m Memory) NoteList(ctx context.Context, cwd string, all bool) ([]memory.Note, error) {
	folder := cwd
	if all {
		folder = ""
	}
	return m.Store.Notes(ctx, folder, m.FoldCase, m.now())
}

// NoteDone deletes a note.
func (m Memory) NoteDone(ctx context.Context, ref string) error {
	return m.Store.DeleteNote(ctx, ref)
}
