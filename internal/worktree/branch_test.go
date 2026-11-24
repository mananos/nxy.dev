package worktree

import (
	"reflect"
	"testing"
)

func TestSuggestBranch(t *testing.T) {
	for _, c := range []struct {
		title    string
		branches []string
		want     []string
	}{
		{"Login Google", nil, []string{"feature/login_google", "fix/login_google"}},
		{"Login Google", []string{"main", "feat/a", "feat/b", "fix/c"}, []string{"feat/login_google", "fix/login_google"}},
		{"Login Google", []string{"main", "fix/a", "feature/b", "feature/c"}, []string{"feature/login_google", "fix/login_google"}},
		{"Login Google", []string{"feature/login_google"}, []string{"fix/login_google"}},
		{"???", nil, nil},
	} {
		if got := SuggestBranch(c.title, c.branches); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q %v: got %v want %v", c.title, c.branches, got, c.want)
		}
	}
}

func TestBaseOptions(t *testing.T) {
	got := BaseOptions([]string{"main", "dev", "main", "x", "y"}, 3)
	if !reflect.DeepEqual(got, []string{"main", "dev", "x"}) {
		t.Errorf("got %v", got)
	}
	if BaseOptions([]string{"main"}, 0) != nil {
		t.Error("max 0")
	}
}

func TestSwitchArgv(t *testing.T) {
	if got := SwitchArgv("b", "main", true); !reflect.DeepEqual(got, []string{"git", "switch", "-c", "b", "main"}) {
		t.Errorf("create %v", got)
	}
	if got := SwitchArgv("b", "main", false); !reflect.DeepEqual(got, []string{"git", "switch", "b"}) {
		t.Errorf("existing %v", got)
	}
}
