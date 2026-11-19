package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/mananos/nxy.dev/v2/internal/views"
)

func TestFitWideAndEllipsis(t *testing.T) {
	// "日本語" is 6 cells: cut to 5 leaves room for 4 (two wide chars) + …
	l := fit(L("日本語"), 5)
	if lw(l) != 5 {
		t.Fatalf("width %d: %v", lw(l), l)
	}
	if got := render(l, newTheme(true)); !strings.HasSuffix(got, "…") {
		t.Fatalf("no ellipsis: %q", got)
	}
	if lw(fit(L("abc"), 10)) != 10 {
		t.Fatal("not padded")
	}
	if got := render(fit(L("abcdef"), 4), newTheme(true)); got != "abc…" {
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
	got := render(lr(L("un título muy largo"), L("$4.62"), 14), newTheme(true))
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
	out := render(lines[0], newTheme(false))
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
		plain := render(l, newTheme(true))
		color := render(l, newTheme(false))
		if ansi.StringWidth(plain) != ansi.StringWidth(color) {
			t.Fatalf("line %d: plain %d, color %d", i, ansi.StringWidth(plain), ansi.StringWidth(color))
		}
		if strings.Contains(plain, "\x1b") {
			t.Fatal("ESC in plain")
		}
	}
	if got := render(lines[0], newTheme(true)); !strings.HasPrefix(got, "›") {
		t.Fatalf("the selected row has no marker: %q", got)
	}
}

func TestStepperAndProgress(t *testing.T) {
	got := render(stepper("plan", "2/4"), newTheme(true))
	for _, want := range []string{"✓ Diseño", "Plan 2/4", "Lotes", "Cierre"} {
		if !strings.Contains(got, want) {
			t.Errorf("%q not in %q", want, got)
		}
	}
	if got := render(progress(3, 4, 12, tWork), newTheme(true)); got != "━━━━━━━━━───" {
		t.Fatalf("progress %q", got)
	}
}

func TestHeadingTakesPadCell(t *testing.T) {
	out := block([]line{L(heading("Te necesita")), L(T("x", ""))}, 30, tBg, 3)
	got := render(out[0], newTheme(true))
	if ansi.StringWidth(got) != 30 {
		t.Fatalf("width %d: %q", ansi.StringWidth(got), got)
	}
	if !strings.HasPrefix(got, "  ▌TE NECESITA") {
		t.Fatalf("heading %q", got)
	}
	if strings.Contains(render(out[1], newTheme(true)), "▌") {
		t.Fatal("▌ on a line that is not a heading")
	}
}

func TestPickPaintsOnlyFirstLine(t *testing.T) {
	lines := []line{L("uno"), L("dos")}
	out := pick(lines, 20, tBg1, true, 2)
	if len(out) != 2 || out[0][0].Bg != tSel || out[1][0].Bg != tBg1 {
		t.Fatalf("backgrounds %q %q", out[0][0].Bg, out[1][0].Bg)
	}
	if !out[0][0].Mark {
		t.Fatal("first line without Mark")
	}
	for _, l := range pick(lines, 20, tBg1, false, 2) {
		if l[0].Bg != tBg1 {
			t.Fatalf("unselected bg %q", l[0].Bg)
		}
	}
}

func TestSelLineInvertsChip(t *testing.T) {
	l := selLine(L(S(" X ", "ch-need"), T("y", tMu)))
	if l[0].Fg != tAc || l[0].Bg != tBg {
		t.Fatalf("chip %q on %q", l[0].Fg, l[0].Bg)
	}
	if l[1].Bg != tSel {
		t.Fatalf("text bg %q", l[1].Bg)
	}
}

func TestChosenBoxAndLoteChip(t *testing.T) {
	st := &state{}
	th := newTheme(false)
	b := views.PlanBatch{N: 3, Title: "algo", Status: views.StepFailed}
	got := render(L(st.batchNode(b, true, 24, "")), th)
	if !strings.Contains(got, "48;2;253;191;1") {
		t.Fatalf("chosen failed box is not yellow: %q", got)
	}
	if other := render(L(st.batchNode(b, false, 24, "")), th); strings.Contains(other, "48;2;253;191;1") {
		t.Fatalf("unchosen box is yellow: %q", other)
	}
	lote := render(L(B(" Lote 3 ", tFail)), th)
	if strings.Contains(lote, "48;2;255;138;128") {
		t.Fatalf("Lote chip has a red background: %q", lote)
	}
}

func TestFillRun(t *testing.T) {
	plain := newTheme(true)
	l := L("ab", spaces(8, ""), spaces(12, ""), "cd")
	got := render(fillRun(l, "╱", tTape, 10, 3), plain)
	want := "ab" + "   " + strings.Repeat("╱", 14) + "   " + "cd"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	short := L("ab", spaces(9, ""), "cd")
	if got := render(fillRun(short, "╱", tTape, 10, 3), plain); got != "ab         cd" {
		t.Fatalf("short run changed: %q", got)
	}
}

func TestAccentUnderlines(t *testing.T) {
	l := accent("ingeniero")
	th := newTheme(false)
	under := th.style(tAc, "", true, true).Render("ingeniero")
	flat := th.style(tAc, "", true, false).Render("ingeniero")
	// lipgloss v2 Render always emits truecolor SGR (the downsampling is on the output writer)
	if !strings.HasPrefix(under, "\x1b[1;4;38;2;253;191;1;") || strings.Contains(flat, ";4;") {
		t.Fatalf("styles render no underline SGR: %q vs %q", under, flat)
	}
	got := render(l, th)
	if !strings.Contains(got, under) || strings.Contains(got, flat) {
		t.Fatalf("label not underlined: got %q want %q", got, under)
	}
	p := render(l, newTheme(true))
	if strings.Contains(p, "\x1b") || p != " ingeniero " || lw(l) != 11 {
		t.Fatalf("plain %q width %d", p, lw(l))
	}
}

func TestFootCap(t *testing.T) {
	s := footCap("q")
	if s.Text != " q " || s.Fg != tAc || s.Bg != tCap || !s.Bold {
		t.Fatalf("%+v", s)
	}
}

func TestTonal(t *testing.T) {
	s := tonal("borrar", tFail, 11)
	if s.Text != " borrar    " || s.Fg != tFail || s.Bg != tFailBg {
		t.Fatalf("%+v", s)
	}
	if got := ansi.StringWidth(tonal("unir", tVer, 11).Text); got != 11 {
		t.Fatalf("width %d", got)
	}
}

func TestFootCapPaints(t *testing.T) {
	got := render(L(footCap("q")), newTheme(false))
	// bold, yellow text on the keycap gray
	if !strings.HasPrefix(got, "\x1b[1;38;2;253;191;1;48;2;42;40;34m q ") {
		t.Fatalf("footCap: %q", got)
	}
}

func TestSelInkOnChosenRow(t *testing.T) {
	th := newTheme(false)
	for fg, want := range map[tok]string{tMu: "38;2;74;59;0", tFail: "38;2;138;28;18", tDone: "38;2;15;90;42"} {
		got := render(L(seg{Text: "x", Fg: fg, Bg: tSel}), th)
		if !strings.Contains(got, want) || !strings.Contains(got, "48;2;253;191;1") {
			t.Errorf("%s on sel: want %s on yellow, got %q", fg, want, got)
		}
	}
	// off the chosen row the plain palette color is used
	if got := render(L(seg{Text: "x", Fg: tMu, Bg: tBg}), th); strings.Contains(got, "38;2;74;59;0") {
		t.Errorf("selInk leaked outside tSel: %q", got)
	}
}

func TestTopBarIsYellow(t *testing.T) {
	st := newState(Options{Width: 120})
	st.data = testData(t)
	if st.decisionCount() == 0 {
		t.Fatal("demo data has no decisions: the chip is not covered")
	}
	bar := st.topBar(120)
	chips := 0
	for _, s := range bar {
		if s.Bg == tBg {
			chips++ // nxy, active tab, decisions chip, cost: dark on the band
			continue
		}
		if s.Bg != tAc {
			t.Fatalf("segment %q on %q, want the yellow band", s.Text, s.Bg)
		}
		if got := render(L(s), st.th); !strings.Contains(got, "48;2;253;191;1") {
			t.Fatalf("segment %q not painted yellow: %q", s.Text, got)
		}
	}
	if chips == 0 {
		t.Fatal("no dark chips on the band")
	}
	if got := render(bar, st.th); !strings.Contains(got, "48;2;253;191;1") {
		t.Fatalf("bar not yellow: %q", got)
	}
}

func TestOverlayKeepsWidth(t *testing.T) {
	th := newTheme(false)
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
