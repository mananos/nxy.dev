package views

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestViewsDecodeStrict(t *testing.T) {
	files, err := filepath.Glob("../../testdata/views/*.json")
	if err != nil || len(files) == 0 {
		t.Fatalf("no view files: %v", err)
	}
	for _, f := range files {
		name := filepath.Base(f)
		id, _, _ := strings.Cut(name, ".")
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			v := New(id)
			if v == nil {
				t.Fatalf("no struct for screen %q", id)
			}
			if err := Decode(data, v); err != nil {
				t.Fatal(err)
			}
			out, err := json.Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			w := New(id)
			if err := Decode(out, w); err != nil {
				t.Fatalf("round trip: %v", err)
			}
			if !reflect.DeepEqual(v, w) {
				t.Fatal("round trip changed the value")
			}
		})
	}
}

func TestViewsCoverScreens(t *testing.T) {
	files, err := filepath.Glob("../../testdata/views/*.json")
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, f := range files {
		id, _, _ := strings.Cut(filepath.Base(f), ".")
		have[id] = true
	}
	known := map[string]bool{}
	for _, id := range Views {
		known[id] = true
		if !have[id] {
			t.Errorf("view %q has no JSON in testdata/views", id)
		}
	}
	for id := range have {
		if !known[id] {
			t.Errorf("JSON for unknown view %q", id)
		}
	}
}

func TestViewsNoPersonalPaths(t *testing.T) {
	files, err := filepath.Glob("../../testdata/views/*.json")
	if err != nil || len(files) == 0 {
		t.Fatalf("no view files: %v", err)
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, bad := range []string{"/home/", "/Users/", `C:\\Users`, `C:\Users`, "C:/Users"} {
			if strings.Contains(string(data), bad) {
				t.Errorf("%s contains %q", filepath.Base(f), bad)
			}
		}
	}
}

func TestDecodeRejects(t *testing.T) {
	if err := Decode([]byte(`{"feature_id":"x","nope":1}`), &Decision{}); err == nil {
		t.Fatal("unknown field accepted")
	}
	if err := Decode([]byte(`{} {}`), &Decision{}); err == nil {
		t.Fatal("trailing data accepted")
	}
}
