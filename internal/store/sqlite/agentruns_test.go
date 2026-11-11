package sqlite

import (
	"testing"
	"time"

	"github.com/mananos/nxy.dev/internal/domain"
)

func TestAgentRuns(t *testing.T) {
	s := mustOpen(t, dbPath(t))
	a := domain.AgentRun{FeatureID: "F1", AgentID: "a2", Role: "implementer", Batch: 2, Calls: 3, TokensIn: 10, CostUSD: 0.5, StartedAt: 200, EndedAt: 300}
	b := domain.AgentRun{FeatureID: "F1", AgentID: "a1", Role: "scout", StartedAt: 100, EndedAt: 150, CostEstimated: true}
	for _, r := range []domain.AgentRun{a, b, {FeatureID: "F2", AgentID: "a1", StartedAt: 1}} {
		if err := s.RecordAgentRun(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.AgentRuns(ctx, "F1")
	if err != nil || len(got) != 2 || got[0] != b || got[1] != a {
		t.Fatalf("runs = %+v %v", got, err)
	}
	a.Calls, a.TokensIn, a.EndedAt = 5, 99, 400
	if err := s.RecordAgentRun(ctx, a); err != nil {
		t.Fatal(err)
	}
	got, _ = s.AgentRuns(ctx, "F1")
	if len(got) != 2 || got[1] != a {
		t.Fatalf("after update = %+v", got)
	}
	if o, _ := s.AgentRuns(ctx, "F2"); len(o) != 1 {
		t.Fatalf("F2 = %+v", o)
	}
}

func TestAgentRunRepeatedStopKeepsData(t *testing.T) {
	s := mustOpen(t, dbPath(t))
	a := domain.AgentRun{FeatureID: "F1", AgentID: "a1", Role: "implementer", Batch: 2, Model: "m", Calls: 3, TokensIn: 10, CostUSD: 0.5, StartedAt: 100, EndedAt: 200}
	if err := s.RecordAgentRun(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordAgentRun(ctx, domain.AgentRun{FeatureID: "F1", AgentID: "a1", Role: "reviewer", EndedAt: 999}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.AgentRuns(ctx, "F1")
	if len(got) != 1 || got[0] != a {
		t.Fatalf("after empty repeat = %+v", got)
	}
}

func TestSweepDropsOldAgentRuns(t *testing.T) {
	s := mustOpen(t, dbPath(t))
	now := time.Now()
	old := now.Add(-agentRunTTL - time.Hour).UnixMilli()
	fresh := now.Add(-time.Hour).UnixMilli()
	s.RecordAgentRun(ctx, domain.AgentRun{FeatureID: "F1", AgentID: "old", StartedAt: old - 5, EndedAt: old})
	s.RecordAgentRun(ctx, domain.AgentRun{FeatureID: "F1", AgentID: "new", StartedAt: fresh - 5, EndedAt: fresh})
	if _, err := s.SweepNotes(ctx, now); err != nil {
		t.Fatal(err)
	}
	got, _ := s.AgentRuns(ctx, "F1")
	if len(got) != 1 || got[0].AgentID != "new" {
		t.Fatalf("after sweep = %+v", got)
	}
}

func TestMigrate11To12AddsAgentRuns(t *testing.T) {
	path := dbPath(t)
	migs, _ := embeddedMigrations()
	s, err := openWith(ctx, path, time.Now, migs[:11])
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	s2 := mustOpen(t, path)
	if err := s2.RecordAgentRun(ctx, domain.AgentRun{FeatureID: "F1", AgentID: "a"}); err != nil {
		t.Fatal(err)
	}
}
