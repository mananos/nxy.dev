package design

import (
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/internal/domain"
)

func forkIn(fork string, n int) ForkInput {
	in := ForkInput{Fork: fork, Opinion: "A, salvo que haya prisa"}
	for i := 0; i < n; i++ {
		in.Options = append(in.Options, domain.ForkOption{Label: "opción " + string(rune('a'+i)), Pros: []string{"p"}, Cons: []string{"c"}})
	}
	return in
}

func TestSetForkOptionsStoresAndMarksCurrent(t *testing.T) {
	d := &domain.Design{}
	SetAgenda(d, []string{"dónde vive el flag", "otra"})
	if p := SetForkOptions(d, forkIn("B1", 3)); p != nil {
		t.Fatal(p)
	}
	f := d.Agenda[0]
	if d.Current != "B1" || len(f.Options) != 3 || f.Options[0].Key != "A" || f.Options[2].Key != "C" {
		t.Fatalf("design: %+v", d)
	}
	if f.Question != "dónde vive el flag" || f.Opinion == "" {
		t.Fatalf("fork: %+v", f)
	}
	if got := pendingState(d); !strings.Contains(got, "B1 dónde vive el flag") || !strings.Contains(got, "- B opción b") {
		t.Fatal(got)
	}
	Propose(d, prop("B1"))
	Approve(d)
	if d.Current != "" {
		t.Fatalf("current kept after the decision: %q", d.Current)
	}
}

func TestSetForkOptionsRefuses(t *testing.T) {
	d := &domain.Design{}
	SetAgenda(d, []string{"a"})
	dup := forkIn("B1", 2)
	dup.Options[0].Key, dup.Options[1].Key = "B", "B"
	fenced := forkIn("B1", 2)
	fenced.Options[0].D2 = "```d2"
	noOp := forkIn("B1", 2)
	noOp.Opinion = " "
	multi := forkIn("B1", 2)
	multi.Options[1].Summary = "uno\ndos"
	for name, in := range map[string]ForkInput{
		"not in agenda": forkIn("B9", 2),
		"one option":    forkIn("B1", 1),
		"five options":  forkIn("B1", 5),
		"repeated key":  dup,
		"fence in d2":   fenced,
		"no opinion":    noOp,
		"multiline":     multi,
	} {
		if p := SetForkOptions(d, in); len(p) == 0 {
			t.Errorf("%s accepted", name)
		}
	}
	if d.Current != "" || len(d.Agenda[0].Options) != 0 {
		t.Fatalf("refused input stored: %+v", d)
	}
}

func TestSetAgendaClearsCurrentWhenItsForkIsGone(t *testing.T) {
	d := &domain.Design{}
	SetAgenda(d, []string{"a"})
	SetForkOptions(d, forkIn("B1", 2))
	SetAgenda(d, []string{"b"})
	if d.Current != "" {
		t.Fatalf("current = %q", d.Current)
	}
}
