package tui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/mananos/nxy.dev/internal/views"
)

// countSrc counts the calls to the views that load on demand and can fail the plan.
type countSrc struct {
	views.Source
	mu      sync.Mutex
	calls   map[string]int
	planErr error
}

func newCountSrc(t *testing.T) *countSrc {
	return &countSrc{Source: testSource(t), calls: map[string]int{}}
}

func (c *countSrc) count(name string) {
	c.mu.Lock()
	c.calls[name]++
	c.mu.Unlock()
}

func (c *countSrc) Agents(ctx context.Context, id string) (views.Agents, error) {
	c.count("Agents")
	return c.Source.Agents(ctx, id)
}

func (c *countSrc) Plan(ctx context.Context, id string) (views.Plan, bool, error) {
	c.count("Plan")
	if c.planErr != nil {
		return views.Plan{}, false, c.planErr
	}
	return c.Source.Plan(ctx, id)
}

func (c *countSrc) File(ctx context.Context, id, p string) (views.File, error) {
	c.count("File")
	return c.Source.File(ctx, id, p)
}

func (c *countSrc) Engineer(ctx context.Context, id string) (views.Engineer, bool, error) {
	c.count("Engineer")
	return c.Source.Engineer(ctx, id)
}

func TestLazyLoad(t *testing.T) {
	src := newCountSrc(t)
	out, err := Render(Options{Source: src, Width: 100, Plain: true}, "inicio", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "F7K2") {
		t.Fatalf("no cockpit in the dump:\n%s", out)
	}
	for _, name := range []string{"Plan", "File", "Engineer", "Agents"} {
		if n := src.calls[name]; n != 0 {
			t.Errorf("Inicio asked for %s %d times", name, n)
		}
	}
	// the screen on top asks for its own view and no other
	if _, err := Render(Options{Source: src, Width: 100, Plain: true}, "plan", "F7K2"); err != nil {
		t.Fatal(err)
	}
	if src.calls["Plan"] != 1 || src.calls["Engineer"] != 0 || src.calls["Agents"] != 0 {
		t.Errorf("calls after Plan: %v", src.calls)
	}
}

func TestViewError(t *testing.T) {
	src := newCountSrc(t)
	src.planErr = errors.New("boom: the plan is broken\nsecond line")
	out, err := Render(Options{Source: src, Width: 100, Plain: true}, "plan", "F7K2")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "No se pudo cargar") || !strings.Contains(out, "boom: the plan is broken") {
		t.Fatalf("no error block in Plan:\n%s", out)
	}
	if strings.Contains(out, "second line") {
		t.Errorf("more than the first line of the error:\n%s", out)
	}
	inicio, err := Render(Options{Source: src, Width: 100, Plain: true}, "inicio", "")
	if err != nil || !strings.Contains(inicio, "F7K2") {
		t.Fatalf("Inicio does not draw: %v\n%s", err, inicio)
	}
}
