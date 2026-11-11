package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestFitWideAndEllipsis(t *testing.T) {
	// "日本語" is 6 cells: cut to 5 leaves room for 4 (two wide chars) + …
	l := fit(L("日本語"), 5)
	if lw(l) != 5 {
		t.Fatalf("width %d: %v", lw(l), l)
	}
	if got := render(l, newTheme(true, true)); !strings.HasSuffix(got, "…") {
		t.Fatalf("no ellipsis: %q", got)
	}
	if lw(fit(L("abc"), 10)) != 10 {
		t.Fatal("not padded")
	}
	if got := render(fit(L("abcdef"), 4), newTheme(true, true)); got != "abc…" {
		t.Fatalf("got %q", got)
	}
}

func TestLrNeverOverflows(t *testing.T) {
	for w := 1; w < 40; w++ {
		for _, rt := range []string{"", "$4.62", "una cola bastante larga de texto"} {
			l := lr(L("título largo de ejemplo", T(" id", tSb)), L(rt), w)
			if n := lw(l); n != w {
				t.Fatalf("w=%d right=%q: width %d", w, rt, n)
			}
		}
	}
	// the right side stays when the left is cut
	got := render(lr(L("un título muy largo"), L("$4.62"), 14), newTheme(true, true))
	if !strings.HasSuffix(got, "$4.62") {
		t.Fatalf("right side lost: %q", got)
	}
}

func TestBlockEverySegmentHasBackground(t *testing.T) {
	lines := block([]line{
		L(chip("Te necesita", tNeed, chipW), "   ", B("título", ""), T(" id", tSb)),
		nil,
	}, 40, tBg1, 3)
	for i, l := range lines {
		if lw(l) != 40 {
			t.Fatalf("line %d is %d wide", i, lw(l))
		}
		for _, s := range l {
			if s.Bg == "" {
				t.Fatalf("line %d: segment %q without background", i, s.Text)
			}
		}
	}
	// painted: a background is set (48;2) in every run of the line
	out := render(lines[0], newTheme(true, false))
	if strings.Count(out, "48;2;") < 3 {
		t.Fatalf("no background in %q", out)
	}
	if ansi.StringWidth(out) != 40 {
		t.Fatalf("painted width %d", ansi.StringWidth(out))
	}
}

func TestPlainMeasuresTheSame(t *testing.T) {
	lines := block([]line{L(chip("Listo", tDone, chipW), "  ", B("x", ""), T("y", tMu))}, 30, tSel, 3)
	for i, l := range lines {
		plain := render(l, newTheme(true, true))
		color := render(l, newTheme(true, false))
		if ansi.StringWidth(plain) != ansi.StringWidth(color) {
			t.Fatalf("line %d: plain %d, color %d", i, ansi.StringWidth(plain), ansi.StringWidth(color))
		}
		if strings.Contains(plain, "\x1b") {
			t.Fatal("ESC in plain")
		}
	}
	if got := render(lines[0], newTheme(true, true)); !strings.HasPrefix(got, "›") {
		t.Fatalf("the selected row has no marker: %q", got)
	}
}

func TestStepperAndProgress(t *testing.T) {
	got := render(stepper("plan", "2/4"), newTheme(true, true))
	for _, want := range []string{"✓ Diseño", "Plan 2/4", "Lotes", "Cierre"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q not in %q", want, got)
		}
	}
	if got := render(progress(3, 4, 12, tWork), newTheme(true, true)); got != "━━━━━━━━━───" {
		t.Fatalf("progress %q", got)
	}
}

func TestOverlayKeepsWidth(t *testing.T) {
	th := newTheme(true, false)
	base := []string{strings.Repeat("a", 20), strings.Repeat("b", 20), "short"}
	out := overlay(base, []string{"XXXX", "YYYY"}, 5, 1)
	if ansi.Strip(out[1]) != "bbbbbXXXXbbbbbbbbbbb" || ansi.Strip(out[0]) != base[0] || ansi.Strip(out[2]) != "shortYYYY" {
		t.Fatalf("got %q %q", ansi.Strip(out[1]), ansi.Strip(out[2]))
	}
	d := dim(base, th)
	if ansi.Strip(d[0]) != base[0] || d[0] == base[0] {
		t.Fatal("dim changed the text or did not paint")
	}
	if centerX(100, 74) != 13 {
		t.Fatal("centerX")
	}
}
