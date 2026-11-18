package sqlite

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/v2/internal/memory"
)

func TestMarkOfferedOnce(t *testing.T) {
	s, _ := newMem(t)
	if ok, err := s.MarkOffered(ctx, "repo-a"); err != nil || !ok {
		t.Fatalf("first %v %v", ok, err)
	}
	if ok, _ := s.MarkOffered(ctx, "repo-a"); ok {
		t.Error("offered twice")
	}
	if ok, _ := s.MarkOffered(ctx, "repo-b"); !ok {
		t.Error("other repo shares the mark")
	}
	// a state saved without an offer still gets its first offer
	if err := s.SaveBootstrap(ctx, BootstrapState{Repo: "repo-c"}); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.MarkOffered(ctx, "repo-c"); !ok {
		t.Error("saved row with offered_at=0 not offered")
	}
}

func TestBootstrapRoundTrip(t *testing.T) {
	s, _ := newMem(t)
	if _, ok, err := s.Bootstrap(ctx, "r"); err != nil || ok {
		t.Fatalf("empty %v %v", ok, err)
	}
	in := BootstrapState{Repo: "r", OfferedAt: 5, Status: "confirming", PendingRequest: "add x", PendingEngineer: true,
		Candidates: []memory.BootstrapCandidate{{Text: "run lint", Question: "q?", Evidence: []string{"Makefile:3"}, Paths: []string{"src/**"}}}}
	if err := s.SaveBootstrap(ctx, in); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Bootstrap(ctx, "r")
	if err != nil || !ok {
		t.Fatalf("%v %v", ok, err)
	}
	a, _ := json.Marshal(in)
	b, _ := json.Marshal(got)
	if string(a) != string(b) {
		t.Errorf("round trip\n%s\n%s", a, b)
	}
	if err := s.SaveBootstrap(ctx, BootstrapState{Repo: "r", Status: "done"}); err != nil {
		t.Fatal(err)
	}
	got, _, _ = s.Bootstrap(ctx, "r")
	if got.Status != "done" || len(got.Candidates) != 0 || got.PendingRequest != "" || got.PendingEngineer {
		t.Errorf("replace %+v", got)
	}
}

func TestBundleExcludesBootstrap(t *testing.T) {
	s, _ := newMem(t)
	if err := s.SaveBootstrap(ctx, BootstrapState{Repo: "r", Status: "confirming", PendingRequest: "secret-request-text",
		Candidates: []memory.BootstrapCandidate{{Text: "cand-text-xyz", Question: "q", Evidence: []string{"a:1"}}}}); err != nil {
		t.Fatal(err)
	}
	b, err := s.ExportBundle(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(b)
	for _, bad := range []string{"secret-request-text", "cand-text-xyz", "bootstrap"} {
		if strings.Contains(string(raw), bad) {
			t.Errorf("bundle carries %q", bad)
		}
	}
}
