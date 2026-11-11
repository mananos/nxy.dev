package platform

import "testing"

func TestDetectIcons(t *testing.T) {
	env := func(kv ...string) func(string) string {
		m := map[string]string{}
		for i := 0; i+1 < len(kv); i += 2 {
			m[kv[i]] = kv[i+1]
		}
		return func(k string) string { return m[k] }
	}
	cases := []struct {
		name   string
		getenv func(string) string
		faces  []string
		fc     string
		want   bool
	}{
		{"override nerd", env("NXY_ICONS", "nerd"), nil, "", true},
		{"override unicode beats wezterm", env("NXY_ICONS", "unicode", "TERM_PROGRAM", "WezTerm"), nil, "Hack Nerd Font", false},
		{"wezterm", env("TERM_PROGRAM", "WezTerm"), nil, "", true},
		{"wt with NF", env("WT_SESSION", "x"), []string{"Consolas", "CaskaydiaCove NF"}, "", true},
		{"wt with Nerd", env("WT_SESSION", "x"), []string{"JetBrainsMono Nerd Font"}, "", true},
		{"wt without", env("WT_SESSION", "x"), []string{"Cascadia Code"}, "", false},
		{"fc-list with nerd", env(), nil, "DejaVu Sans\nHack Nerd Font Mono\n", true},
		{"fc-list without", env(), nil, "DejaVu Sans\nNoto Sans\n", false},
		{"nothing", env(), nil, "", false},
	}
	for _, c := range cases {
		if got := IconsFrom(c.getenv, c.faces, c.fc); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
