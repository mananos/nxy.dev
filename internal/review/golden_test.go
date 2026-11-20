package review

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type goldenLens struct {
	Name  string   `json:"name"`
	Files []string `json:"files"`
}

type goldenCase struct {
	Name  string `json:"name"`
	Input struct {
		Repo []struct {
			Name string `json:"name"`
			Text string `json:"text"`
		} `json:"repo"`
		Files []struct {
			Path    string  `json:"path"`
			Content *string `json:"content"`
		} `json:"files"`
	} `json:"input"`
	Output struct {
		Lenses []goldenLens `json:"lenses"`
	} `json:"output"`
}

func TestGoldenLenses(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "golden", "lenses", "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []goldenCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 20 {
		t.Fatalf("expected the whole corpus, got %d cases", len(cases))
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			var repo []Lens
			for _, r := range c.Input.Repo {
				if l, ok := ParseLens(r.Text, r.Name); ok {
					repo = append(repo, l)
				}
			}
			var files []File
			for _, f := range c.Input.Files {
				file := File{Path: f.Path, Deleted: f.Content == nil}
				if f.Content != nil {
					file.Content = *f.Content
				}
				files = append(files, file)
			}
			got := []goldenLens{}
			for _, s := range Select(Merge(Builtin(), repo), files) {
				fs := s.Files
				if fs == nil {
					fs = []string{}
				}
				got = append(got, goldenLens{Name: s.Lens.Name, Files: fs})
			}
			want := c.Output.Lenses
			if want == nil {
				want = []goldenLens{}
			}
			// the 1.x order is the directory order; compare as sets by name
			if !reflect.DeepEqual(byName(got), byName(want)) {
				t.Fatalf("got %v, want %v", got, want)
			}
		})
	}
}

func byName(ls []goldenLens) map[string][]string {
	m := map[string][]string{}
	for _, l := range ls {
		m[l.Name] = l.Files
	}
	return m
}
