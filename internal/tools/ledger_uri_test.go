package tools

import "testing"

func TestReadOnlyURI(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"C:/Users/me/history.db", "file:///C:/Users/me/history.db?mode=ro"},
		{"/home/me/a b/h?x#y%z.db", "file:///home/me/a%20b/h%3Fx%23y%25z.db?mode=ro"},
		{"//server/share/x.db", "file:////server/share/x.db?mode=ro"},
		{"/var/lib/rtk/history.db", "file:///var/lib/rtk/history.db?mode=ro"},
	} {
		if got := readOnlyURI(c.in); got != c.want {
			t.Errorf("readOnlyURI(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
