package app

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mananos/nxy.dev/v2/internal/memory"
)

type memFake struct {
	repos        []string
	deleted      []string
	learnings    []memory.Learning
	exportNotes  []bool
	applied      []memory.Diff
	snapshotRuns int
	snap         memory.Snapshot
}

func (f *memFake) AddRule(_ context.Context, r memory.Rule) (memory.Rule, error) {
	f.repos = append(f.repos, r.Repo)
	if err := memory.ValidateRule(r); err != nil {
		return memory.Rule{}, err
	}
	return r, nil
}
func (f *memFake) Rules(_ context.Context, repo string) ([]memory.Rule, error) {
	f.repos = append(f.repos, repo)
	return nil, nil
}
func (f *memFake) AddGlobalRule(_ context.Context, r memory.Rule) (memory.Rule, error) {
	f.repos = append(f.repos, r.Repo)
	r.Repo = memory.GlobalRepo
	if err := memory.ValidateRule(r); err != nil {
		return memory.Rule{}, err
	}
	return r, nil
}
func (f *memFake) GlobalRules(context.Context) ([]memory.Rule, error) { return nil, nil }
func (f *memFake) EditRule(_ context.Context, ref string, _ memory.RuleEdit) (memory.Rule, error) {
	return memory.Rule{Short: ref}, nil
}
func (f *memFake) EditLearning(_ context.Context, ref string, _ memory.LearningEdit) (memory.Learning, error) {
	return memory.Learning{Short: ref}, nil
}
func (f *memFake) DeleteMemory(_ context.Context, ref string) (string, error) {
	f.deleted = append(f.deleted, ref)
	return ref, nil
}
func (f *memFake) KeepMemory(_ context.Context, ref string) (string, error) { return ref, nil }
func (f *memFake) MemoryKind(_ context.Context, ref string) (string, error) {
	if strings.HasPrefix(strings.ToUpper(ref), "A") {
		return "learning", nil
	}
	return "rule", nil
}
func (f *memFake) RestoreMemory(_ context.Context, ref string) (string, error) { return ref, nil }
func (f *memFake) AddLearning(_ context.Context, l memory.Learning) (memory.Learning, error) {
	f.repos = append(f.repos, l.Repo)
	l.Hash = memory.LearningHash(l.Text, l.Files)
	for i := range f.learnings {
		if f.learnings[i].Hash == l.Hash {
			f.learnings[i].Seen++
			return f.learnings[i], nil
		}
	}
	l.Seen = 1
	f.learnings = append(f.learnings, l)
	return l, nil
}
func (f *memFake) Learnings(_ context.Context, repo string) ([]memory.Learning, error) {
	return f.learnings, nil
}
func (f *memFake) Promote(context.Context, string, string, []string) (memory.Rule, error) {
	return memory.Rule{}, nil
}
func (f *memFake) Related(_ context.Context, repo string, _ []string) ([]memory.ClosedFeature, []memory.Learning, error) {
	f.repos = append(f.repos, repo)
	return nil, nil, nil
}
func (f *memFake) Search(_ context.Context, repo string, _ bool, _ string, _ int) ([]memory.Hit, error) {
	f.repos = append(f.repos, repo)
	return nil, nil
}
func (f *memFake) SaveNote(_ context.Context, n memory.Note) (memory.Note, error) { return n, nil }
func (f *memFake) Notes(_ context.Context, _ string, _ bool, _ time.Time) ([]memory.Note, error) {
	return nil, nil
}
func (f *memFake) DeleteNote(context.Context, string) error { return nil }
func (f *memFake) ExportBundle(_ context.Context, includeNotes bool) (memory.Bundle, error) {
	f.exportNotes = append(f.exportNotes, includeNotes)
	return memory.Bundle{Version: memory.BundleVersion}, nil
}
func (f *memFake) MemorySnapshot(context.Context) (memory.Snapshot, error) {
	f.snapshotRuns++
	return f.snap, nil
}
func (f *memFake) ApplyImport(_ context.Context, d memory.Diff) error {
	f.applied = append(f.applied, d)
	return nil
}

func newMemory(f *memFake) Memory { return Memory{Store: f, Repos: fakeRepos{}} }

func TestMemoryRepoKeyReachesTheStore(t *testing.T) {
	f := &memFake{}
	m := newMemory(f)
	ctx := context.Background()
	if _, err := m.AddRule(ctx, "/w", "use tabs", "indent?", "", nil, "", ""); err != nil {
		t.Fatal(err)
	}
	m.Show(ctx, "/w")
	m.Search(ctx, "/w", "tabs", false)
	m.Related(ctx, "/w", []string{"a.go"})
	for _, r := range f.repos {
		if r != "repo:/w" {
			t.Fatalf("repo key = %q", r)
		}
	}
	if len(f.repos) != 4 {
		t.Fatalf("calls = %d", len(f.repos))
	}
	if _, err := m.AddRule(ctx, "/w", "no question", " ", "", nil, "", ""); err == nil {
		t.Fatal("a rule without a question was accepted")
	}
}

func TestMemoryAddLearningTwiceCountsSeen(t *testing.T) {
	m := newMemory(&memFake{})
	ctx := context.Background()
	m.AddLearning(ctx, "/w", "retry is idempotent", []string{"a.go"})
	l, err := m.AddLearning(ctx, "/w", " Retry is idempotent ", []string{"a.go"})
	if err != nil || l.Seen != 2 {
		t.Fatalf("seen = %d, err = %v", l.Seen, err)
	}
}

func TestMemoryExportAsksForNotesOnlyWhenTold(t *testing.T) {
	f := &memFake{}
	m := newMemory(f)
	m.Export(context.Background(), false)
	m.Export(context.Background(), true)
	if len(f.exportNotes) != 2 || f.exportNotes[0] || !f.exportNotes[1] {
		t.Fatalf("includeNotes = %v", f.exportNotes)
	}
}

func TestMemoryImportPreviewWritesNothingApplyAppliesTheSameDiff(t *testing.T) {
	f := &memFake{}
	m := newMemory(f)
	ctx := context.Background()
	in, err := memory.EncodeBundle(memory.Bundle{
		Version: memory.BundleVersion,
		Rules: []memory.Rule{{
			Meta: memory.Meta{ID: "01AAAAAAAAAAAAAAAAAAAAAAAA", CreatedAt: 1, UpdatedAt: 1},
			Repo: "repo:/w", Text: "use tabs", Question: "indent?", Origin: memory.OriginManual,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	d, err := m.ImportPreview(ctx, bytes.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.applied) != 0 || d.Count(memory.New) != 1 {
		t.Fatalf("preview wrote %d times, new = %d", len(f.applied), d.Count(memory.New))
	}
	if err := m.ImportApply(ctx, d); err != nil {
		t.Fatal(err)
	}
	if len(f.applied) != 1 || len(f.applied[0].Rules) != 1 {
		t.Fatalf("applied = %+v", f.applied)
	}
	if err := m.ImportApply(ctx, memory.Diff{}); err != nil || len(f.applied) != 1 {
		t.Fatal("an empty diff must not write")
	}
	if _, err := m.ImportPreview(ctx, strings.NewReader(`{"version":2}`)); err == nil {
		t.Fatal("version 2 accepted")
	}
}
