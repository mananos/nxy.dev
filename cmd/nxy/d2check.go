package main

import (
	"context"
	"errors"
	"os"
	"sync"

	"github.com/mananos/nxy.dev/v2/internal/app"
	"github.com/mananos/nxy.dev/v2/internal/tools"
)

// d2Tester draws a d2 source with the real d2; with no d2 or a timeout it says nothing.
type d2Tester struct {
	once  sync.Once
	bin   string
	found bool
}

func (t *d2Tester) CheckD2(ctx context.Context, src string) error {
	t.once.Do(func() {
		var p string
		p, _, t.found = tools.FindD2(os.Getenv)
		t.bin = p
	})
	if !t.found {
		return nil
	}
	_, err := tools.RenderD2(ctx, t.bin, src)
	if err == nil || errors.Is(err, context.DeadlineExceeded) {
		return nil
	}
	return err
}

// d2Check is the checker the hooks use; a var so tests of cmd can replace it.
var d2Check app.D2Check = &d2Tester{}
