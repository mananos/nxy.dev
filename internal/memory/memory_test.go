package memory

import (
	"strings"
	"testing"
	"time"
)

func goodRule() Rule {
	return Rule{Meta: Meta{ID: "R1", UpdatedAt: 10}, Short: "K1", Repo: "r", Text: "use tabs", Question: "tabs or spaces?", Origin: OriginManual}
}

func TestValidateRule(t *testing.T) {
	bad := map[string]func(*Rule){
		"no question": func(r *Rule) { r.Question = "  " },
		"no text":     func(r *Rule) { r.Text = "" },
		"bad origin":  func(r *Rule) { r.Origin = "dream" },
		"bad glob":    func(r *Rule) { r.Paths = []string{"[a"} },
	}
	for name, mut := range bad {
		r := goodRule()
		mut(&r)
		if ValidateRule(r) == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	r := goodRule()
	r.Question = ""
	if err := ValidateRule(r); err == nil || !strings.Contains(err.Error(), "la pregunta que resuelve") {
		t.Errorf("err = %v", err)
	}
	r = goodRule()
	r.Paths = []string{"internal/*/x.go"}
	if err := ValidateRule(r); err != nil {
		t.Errorf("good rule: %v", err)
	}
}

func TestValidateNote(t *testing.T) {
	if ValidateNote(Note{Title: "t", Body: strings.Repeat("a", NoteMaxBytes)}) != nil {
		t.Error("2048 B rejected")
	}
	if ValidateNote(Note{Title: "t", Body: strings.Repeat("a", NoteMaxBytes+1)}) == nil {
		t.Error("2049 B accepted")
	}
	if ValidateNote(Note{Title: " "}) == nil {
		t.Error("empty title accepted")
	}
}

func TestLearningHash(t *testing.T) {
	a := LearningHash("Use  Tabs.", []string{"b/x.go", "./a/y.go"})
	b := LearningHash("use tabs", []string{"a/y.go", `b\x.go`})
	if a != b {
		t.Errorf("hashes differ: %s %s", a, b)
	}
	if a == LearningHash("use tabs", []string{"a/y.go"}) {
		t.Error("different files, same hash")
	}
}

func TestCleanPath(t *testing.T) {
	for in, want := range map[string]string{"./a/b": "a/b", `a\b\..\c`: "a/c", "": "", "a//b/": "a/b"} {
		if got := CleanPath(in); got != want {
			t.Errorf("CleanPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFolderMatch(t *testing.T) {
	cases := []struct {
		a, b string
		fold bool
		want bool
	}{
		{"/a/b", "/a/b", false, true},
		{"/a/b/c", "/a/b", false, true},
		{"/a/b", "/a/b/c", false, true},
		{"/a/b", "/a/bc", false, false},
		{"/a/B", "/a/b", false, false},
		{`C:\Work\Repo`, `c:/work/repo/sub`, true, true},
		{`C:\Work\Repo`, `c:/work/repo/sub`, false, false},
	}
	for _, c := range cases {
		if got := FolderMatch(c.a, c.b, c.fold); got != c.want {
			t.Errorf("FolderMatch(%q,%q,%v) = %v", c.a, c.b, c.fold, got)
		}
	}
}

func TestExpiry(t *testing.T) {
	base := time.UnixMilli(1_000_000_000)
	n := Note{Meta: Meta{UpdatedAt: base.UnixMilli()}}
	if NoteExpired(n, base.Add(NoteTTL-time.Millisecond)) {
		t.Error("expired too soon")
	}
	if !NoteExpired(n, base.Add(NoteTTL)) {
		t.Error("not expired at 30 days")
	}
	r := Rule{Meta: Meta{CreatedAt: base.UnixMilli()}}
	if NeedsReview(r, base.Add(ReviewAfter-time.Millisecond)) || !NeedsReview(r, base.Add(ReviewAfter)) {
		t.Error("review edge wrong")
	}
	r.LastCitedAt = base.Add(ReviewAfter).UnixMilli()
	if NeedsReview(r, base.Add(ReviewAfter)) {
		t.Error("a citation should push the review date")
	}
}

func TestFTSQuery(t *testing.T) {
	if got := FTSQuery(`say "hi" now`); got != `"say" AND """hi""" AND "now"` {
		t.Errorf("got %s", got)
	}
	if FTSQuery("  ") != "" {
		t.Error("blank query not empty")
	}
}

func TestPathMatches(t *testing.T) {
	if !PathMatches("a/b/c.go", "a/b") || !PathMatches("a/b.go", "./a/b.go") {
		t.Error("should match")
	}
	if PathMatches("a/bc.go", "a/b") || PathMatches("a", "a/b") || PathMatches("", "") {
		t.Error("should not match")
	}
}

func TestDecodeBundle(t *testing.T) {
	for _, in := range []string{`{"version":2}`, `{}`} {
		_, err := DecodeBundle([]byte(in))
		if err == nil || !strings.Contains(err.Error(), "versión de exportación") || !strings.Contains(err.Error(), "este nxy lee la 1") {
			t.Errorf("%s: err = %v", in, err)
		}
	}
	noQ := `{"version":1,"rules":[{"id":"R1","repo":"r","text":"t","origin":"manual"}]}`
	if _, err := DecodeBundle([]byte(noQ)); err == nil {
		t.Error("rule without question accepted")
	}
	tomb := `{"version":1,"rules":[{"id":"R1","deleted_at":5}]}`
	if _, err := DecodeBundle([]byte(tomb)); err != nil {
		t.Errorf("tombstone: %v", err)
	}
}

func TestDecodeBundleValidatesLikeTheCLI(t *testing.T) {
	noText := `{"version":1,"learnings":[{"id":"L1","repo":"r"}]}`
	if _, err := DecodeBundle([]byte(noText)); err == nil {
		t.Error("learning without text accepted")
	}
	noTitle := `{"version":1,"features":[{"id":"F1"}]}`
	if _, err := DecodeBundle([]byte(noTitle)); err == nil {
		t.Error("feature without title accepted")
	}
	in := `{"version":1,"rules":[{"id":"R1","short":"K1; DROP","repo":"r","text":"t","question":"q","origin":"manual"}],` +
		`"learnings":[{"id":"L1","short":"A2","repo":"r","text":"Use Tabs.","files":["a.go"],"hash":"bogus"}],` +
		`"notes":[{"id":"N1","short":"X3","title":"t"}]}`
	b, err := DecodeBundle([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if b.Rules[0].Short != "" || b.Notes[0].Short != "" {
		t.Errorf("invalid shorts kept: %q %q", b.Rules[0].Short, b.Notes[0].Short)
	}
	if b.Learnings[0].Short != "A2" {
		t.Errorf("valid short dropped: %q", b.Learnings[0].Short)
	}
	if want := LearningHash("use tabs", []string{"a.go"}); b.Learnings[0].Hash != want {
		t.Errorf("hash = %q, want %q", b.Learnings[0].Hash, want)
	}
}

func TestValidateRuleRoles(t *testing.T) {
	g := goodRule()
	g.Repo, g.Short, g.Roles = GlobalRepo, "G1", []string{RolePlanner, RoleReviewer}
	if err := ValidateRule(g); err != nil {
		t.Errorf("global rule with roles: %v", err)
	}
	g.Roles = []string{"scout"}
	if err := ValidateRule(g); err == nil {
		t.Error("unknown role accepted")
	}
	k := goodRule()
	k.Roles = []string{RolePlanner}
	if err := ValidateRule(k); err == nil || !strings.Contains(err.Error(), "sólo se aplica a reglas globales") {
		t.Errorf("roles on a K: %v", err)
	}
	got := CleanRoles([]string{" Reviewer", "planner", "reviewer", ""})
	if len(got) != 2 || got[0] != "planner" || got[1] != "reviewer" {
		t.Errorf("CleanRoles = %v", got)
	}
}

func TestDecodeBundleGlobals(t *testing.T) {
	in := `{"version":1,"rules":[` +
		`{"id":"R1","short":"G2","text":"t","question":"q","origin":"manual","roles":["planner"]},` +
		`{"id":"R2","short":"K3","text":"t","question":"q","origin":"manual"},` +
		`{"id":"R3","short":"K4","repo":"r","text":"t","question":"q","origin":"manual"},` +
		`{"id":"R4","short":"G5","repo":"r","text":"t","question":"q","origin":"manual"}],` +
		`"learnings":[{"id":"L1","short":"A1","text":"global one"}]}`
	b, err := DecodeBundle([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if b.Rules[0].Short != "G2" || b.Rules[1].Short != "" || b.Rules[2].Short != "K4" || b.Rules[3].Short != "" {
		t.Errorf("shorts: %q %q %q %q", b.Rules[0].Short, b.Rules[1].Short, b.Rules[2].Short, b.Rules[3].Short)
	}
	if b.Learnings[0].Repo != GlobalRepo || b.Learnings[0].Hash == "" {
		t.Errorf("global learning: %+v", b.Learnings[0])
	}
	bad := `{"version":1,"rules":[{"id":"R1","repo":"r","text":"t","question":"q","origin":"manual","roles":["planner"]}]}`
	if _, err := DecodeBundle([]byte(bad)); err == nil {
		t.Error("roles on a repo rule accepted")
	}
}

func TestDecodeBundleBlankRepo(t *testing.T) {
	rule := `{"version":1,"rules":[{"id":"R1","repo":"  ","text":"t","question":"q","origin":"manual"}]}`
	if _, err := DecodeBundle([]byte(rule)); err == nil {
		t.Error("rule with a blank repo accepted")
	}
	learning := `{"version":1,"learnings":[{"id":"L1","repo":"  ","text":"t"}]}`
	if _, err := DecodeBundle([]byte(learning)); err == nil {
		t.Error("learning with a blank repo accepted")
	}
}

func TestEncodeBundleRoundTrip(t *testing.T) {
	b := Bundle{ExportedAt: 5, Rules: []Rule{{Meta: Meta{ID: "B"}, Repo: "r", Text: "t", Question: "q", Origin: OriginChat}, {Meta: Meta{ID: "A"}, Repo: "r", Text: "t", Question: "q", Origin: OriginChat}}}
	out, err := EncodeBundle(b)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, `"notes"`) {
		t.Error("notes key present without notes")
	}
	if strings.Index(s, `"A"`) > strings.Index(s, `"B"`) {
		t.Error("not ordered by id")
	}
	got, err := DecodeBundle(out)
	if err != nil || len(got.Rules) != 2 || got.Rules[0].ID != "A" {
		t.Errorf("round trip: %v %+v", err, got.Rules)
	}
	again, _ := EncodeBundle(got)
	if string(again) != s {
		t.Error("encode is not stable")
	}
}

func TestMergeMarksCounters(t *testing.T) {
	local := Snapshot{
		Rules:     []Rule{{Meta: Meta{ID: "R1", UpdatedAt: 10}, Deliveries: 3}, {Meta: Meta{ID: "R2", UpdatedAt: 10}, Deliveries: 3}},
		Learnings: []Learning{{Meta: Meta{ID: "L1", UpdatedAt: 10}, Repo: "r", Hash: "h", Seen: 2}},
	}
	in := Bundle{
		Rules:     []Rule{{Meta: Meta{ID: "R1", UpdatedAt: 5}, Deliveries: 4}, {Meta: Meta{ID: "R2", UpdatedAt: 5}, Deliveries: 3}},
		Learnings: []Learning{{Meta: Meta{ID: "L1", UpdatedAt: 5}, Repo: "r", Hash: "h", Seen: 3}},
	}
	d := Merge(local, in)
	if !d.Rules[0].Counters || d.Rules[1].Counters || !d.Learnings[0].Counters || d.Writes() != 2 {
		t.Errorf("counters: %+v %+v", d.Rules, d.Learnings)
	}
}

func TestMerge(t *testing.T) {
	local := Snapshot{
		Rules: []Rule{
			{Meta: Meta{ID: "R1", UpdatedAt: 10}, Short: "K1"},
			{Meta: Meta{ID: "R2", UpdatedAt: 10}, Short: "K2"},
			{Meta: Meta{ID: "R3", UpdatedAt: 10}, Short: "K3"},
		},
		Learnings: []Learning{{Meta: Meta{ID: "L1"}, Short: "A1", Repo: "r", Hash: "h", Seen: 1}},
	}
	in := Bundle{
		Rules: []Rule{
			{Meta: Meta{ID: "R1", UpdatedAt: 20}, Short: "K1"},                // newer
			{Meta: Meta{ID: "R2", UpdatedAt: 5}, Short: "K2"},                 // older
			{Meta: Meta{ID: "R3", UpdatedAt: 30, DeletedAt: 30}, Short: "K3"}, // delete
			{Meta: Meta{ID: "R4", UpdatedAt: 1}, Short: "K1"},                 // new, short clash
			{Meta: Meta{ID: "R5", UpdatedAt: 1, DeletedAt: 1}},                // unknown tombstone
		},
		Learnings: []Learning{
			{Meta: Meta{ID: "L9"}, Short: "A1", Repo: "r", Hash: "h", Seen: 2}, // folds
			{Meta: Meta{ID: "L8"}, Short: "A2", Repo: "r", Hash: "other"},      // new
		},
	}
	d := Merge(local, in)
	kinds := func() string {
		var s []string
		for _, c := range d.Rules {
			s = append(s, string(c.Kind))
		}
		return strings.Join(s, ",")
	}
	if kinds() != "changed,skipped,changed,new,new" {
		t.Errorf("rules: %s", kinds())
	}
	if !d.Rules[2].Deleted || !d.Rules[3].ReassignShort || d.Rules[0].ReassignShort {
		t.Errorf("flags: %+v", d.Rules)
	}
	if d.Learnings[0].Kind != Folded && d.Learnings[1].Kind != Folded {
		t.Errorf("no fold: %+v", d.Learnings)
	}
	for _, c := range d.Learnings {
		if c.Row.ID == "L9" && (c.Kind != Folded || c.FoldInto != "L1") {
			t.Errorf("L9: %+v", c)
		}
		if c.Row.ID == "L8" && c.Kind != New {
			t.Errorf("L8: %+v", c)
		}
	}

	// A second pass over the applied state skips everything (a tombstone is
	// known now, so it does not revive).
	applied := Snapshot{Rules: []Rule{
		{Meta: Meta{ID: "R1", UpdatedAt: 20}, Short: "K1"},
		{Meta: Meta{ID: "R2", UpdatedAt: 10}, Short: "K2"},
		{Meta: Meta{ID: "R3", UpdatedAt: 30, DeletedAt: 30}, Short: "K3"},
		{Meta: Meta{ID: "R4", UpdatedAt: 1}, Short: "K4"},
		{Meta: Meta{ID: "R5", UpdatedAt: 1, DeletedAt: 1}},
	}}
	in.Learnings = nil
	d2 := Merge(applied, in)
	if d2.Writes() != 0 || d2.Count(Skipped) != 5 {
		t.Errorf("second pass: %+v", d2.Rules)
	}
}
