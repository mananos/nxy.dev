package repos

import "testing"

func TestSplit(t *testing.T) {
	names := []string{"api", "clientX/api", "web"}
	cases := []struct {
		at, name, rel string
		ok            bool
	}{
		{"api/src/X.java:12", "api", "src/X.java:12", true},
		{"clientX/api/src/X.java", "clientX/api", "src/X.java", true},
		{"web", "web", "", true},
		{"webx/a.js", "", "", false},
		{"src/a.go", "", "", false},
		{"web\\a.js", "web", "a.js", true},
	}
	for _, c := range cases {
		n, r, ok := Split(c.at, names)
		if n != c.name || r != c.rel || ok != c.ok {
			t.Errorf("Split(%q) = %q %q %v", c.at, n, r, ok)
		}
	}
}

func TestLabelNoRepo(t *testing.T) {
	if got := Label("", "/x/y\\z", nil, false); got != "/x/y/z" {
		t.Errorf("got %q", got)
	}
}

func TestNamesFullPathFallback(t *testing.T) {
	n := Names([]string{"/x/Api", "/x/api"}, true)
	if n["/x/Api"] != "/x/Api" || n["/x/api"] != "/x/api" {
		t.Errorf("got %v", n)
	}
}
