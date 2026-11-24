package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"

	"github.com/mananos/nxy.dev/v2/internal/views"
)

// d2Cache draws d2 sources once: it finds d2 on first use and keeps each drawing by the hash of its
// source and the version of d2. Without d2, or when a render fails, a diagram keeps its source and no lines.
type d2Cache struct {
	tools  func() viewToolset // nil: the package replacement (tests)
	mu     sync.Mutex
	looked bool
	found  bool
	bin    string
	ver    string
	lines  map[string][]string
	failed map[string]string // sources d2 could not draw, with its message: tried once per source and version
}

func (c *d2Cache) draw(ctx context.Context, src string) views.Diagram {
	empty := views.Diagram{Source: src, Lines: []string{}}
	c.mu.Lock()
	defer c.mu.Unlock()
	ts := viewTools
	if c.tools != nil {
		ts = c.tools()
	}
	if !c.looked {
		c.looked = true
		if ts.FindD2 != nil {
			c.bin, c.ver, c.found = ts.FindD2()
		}
	}
	if !c.found || ts.RenderD2 == nil {
		empty.Problem = "missing"
		return empty
	}
	sum := sha256.Sum256([]byte(src))
	key := hex.EncodeToString(sum[:]) + "|" + c.ver
	if msg, bad := c.failed[key]; bad {
		empty.Problem, empty.Error = "error", msg
		return empty
	}
	lines, ok := c.lines[key]
	if !ok {
		var err error
		if lines, err = ts.RenderD2(ctx, c.bin, src); err != nil {
			if errors.Is(err, context.Canceled) {
				return empty // the caller gave up: not a failure of the source, try again next time
			}
			if errors.Is(err, context.DeadlineExceeded) {
				empty.Problem = "slow" // d2 took too long: not kept, the next load tries again
				return empty
			}
			if c.failed == nil {
				c.failed = map[string]string{}
			}
			c.failed[key] = err.Error()
			empty.Problem, empty.Error = "error", err.Error()
			return empty
		}
		if c.lines == nil {
			c.lines = map[string][]string{}
		}
		c.lines[key] = lines
	}
	if lines == nil {
		lines = []string{}
	}
	return views.Diagram{Source: src, Lines: append([]string{}, lines...), Renderer: "d2 " + c.ver + " txt"}
}

func (v *viewSource) d2cache() *d2Cache {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.d2 == nil {
		v.d2 = &d2Cache{tools: v.tools}
	}
	return v.d2
}

// Engineer is the screen of engineer mode: the agenda, the options of the current fork and the
// diagrams drawn by d2 when it is installed.
func (v *viewSource) Engineer(ctx context.Context, featureID string) (views.Engineer, bool, error) {
	row, err := v.row(ctx, featureID)
	if err != nil {
		return views.Engineer{}, false, err
	}
	if row.Feature.Design == nil {
		return views.Engineer{}, false, nil
	}
	fd, err := v.featureData(ctx, row)
	if err != nil {
		return views.Engineer{}, false, err
	}
	waiting, err := v.store.OpenPendingAsks(ctx)
	if err != nil {
		return views.Engineer{}, false, err
	}
	cache := v.d2cache()
	e, ok := views.BuildEngineer(views.EngineerIn{
		FeatureIn: fd.featureIn(v.now()),
		Waiting:   waiting,
		Draw:      func(src string) views.Diagram { return cache.draw(ctx, src) },
	})
	return e, ok, nil
}
