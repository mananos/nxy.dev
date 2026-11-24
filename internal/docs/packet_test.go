package docs

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mananos/nxy.dev/v2/internal/domain"
)

func bigDiff(name string, lines int) string {
	var b strings.Builder
	b.WriteString("diff --git a/" + name + " b/" + name)
	for i := 0; i < lines; i++ {
		fmt.Fprintf(&b, "\n+line %d of %s with some padding text to weigh bytes", i, name)
	}
	return b.String()
}

func TestPacketFitsBudget(t *testing.T) {
	in := PacketIn{
		ID:   "r1",
		Plan: strings.Repeat("plan text line\n", 2000),
		Files: []File{
			{Path: "a_test.go", Status: "M", Diff: bigDiff("a_test.go", 20)},
			{Path: "README.md", Status: "M", Diff: bigDiff("README.md", 20)},
			{Path: "main.go", Status: "M", Diff: bigDiff("main.go", 20)},
			{Path: "big.go", Status: "M", Diff: bigDiff("big.go", 400)},
		},
	}
	got := Packet(in)
	if len(got) > 25000 {
		t.Fatalf("packet is %d bytes, over 25000", len(got))
	}
	if !strings.Contains(got, "(plan cut:") {
		t.Errorf("missing plan cut note")
	}
	if !strings.Contains(got, "(diff of big.go left out") {
		t.Errorf("missing left out note for big.go")
	}
	code := strings.Index(got, "diff --git a/main.go")
	doc := strings.Index(got, "diff --git a/README.md")
	test := strings.Index(got, "diff --git a/a_test.go")
	if code < 0 || doc < 0 || test < 0 || !(code < doc && doc < test) {
		t.Errorf("order wrong: code=%d doc=%d test=%d", code, doc, test)
	}

	small := Packet(PacketIn{ID: "r2", Plan: strings.Repeat("x", 10001)})
	if !strings.Contains(small, "(plan cut:") {
		t.Errorf("plan over 10000 bytes not cut")
	}
	if strings.Contains(Packet(PacketIn{ID: "r3", Plan: "short"}), "(plan cut:") {
		t.Errorf("short plan cut")
	}
}

func TestPacketCapsHeaderAndNotes(t *testing.T) {
	var files []File
	var ds []domain.DocFile
	for i := 0; i < 600; i++ {
		files = append(files, File{Path: fmt.Sprintf("pkg/file_%d.go", i), Status: "M", Diff: bigDiff(fmt.Sprintf("pkg/file_%d.go", i), 3)})
	}
	for i := 0; i < 40; i++ {
		d := domain.DocFile{Path: fmt.Sprintf("docs/d%d.md", i)}
		for j := 0; j < 50; j++ {
			d.Lines = append(d.Lines, domain.DocLine{Line: j + 1, Text: strings.Repeat("word ", 20)})
		}
		ds = append(ds, d)
	}
	got := Packet(PacketIn{ID: "r4", Plan: strings.Repeat("plan\n", 5000), Files: files, Docs: ds})
	if len(got) > 25000 {
		t.Fatalf("packet is %d bytes, over 25000", len(got))
	}
}
