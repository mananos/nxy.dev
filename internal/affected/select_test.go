package affected

import (
	"reflect"
	"testing"
)

type fakeTree map[string]string

func (t fakeTree) Read(p string) ([]byte, bool) {
	s, ok := t[p]
	return []byte(s), ok
}

// pair is a Command without its origin, for the golden strings.
type pair struct{ Dir, Line string }

func plain(rows []Runner, tests []string, tree Tree) ([]pair, []string) {
	cmds, un := Commands(rows, tests, tree)
	var out []pair
	for _, c := range cmds {
		out = append(out, pair{c.Dir, c.Line})
	}
	return out, un
}

func TestCommandsNameTheRow(t *testing.T) {
	repo := Runner{Name: "mine", Detect: []Detect{{File: "go.mod"}}, Tests: []string{"*_test.go"}, Command: "x {tests}", Item: "dir"}
	cmds, _ := Commands(Merge([]Runner{repo}, Builtin()), []string{"a_test.go"}, fakeTree{"go.mod": ""})
	if len(cmds) != 1 || cmds[0].Row != "mine" || !cmds[0].Repo {
		t.Errorf("cmds = %+v", cmds)
	}
	cmds, _ = Commands(Builtin(), []string{"a_test.go"}, fakeTree{"go.mod": ""})
	if len(cmds) != 1 || cmds[0].Row != "go" || cmds[0].Repo {
		t.Errorf("cmds = %+v", cmds)
	}
}

func TestCommandsPerRow(t *testing.T) {
	rows := Builtin()
	cases := []struct {
		name  string
		tree  fakeTree
		tests []string
		want  []pair
	}{
		{"go", fakeTree{"go.mod": ""}, []string{"internal/x/a_test.go", "internal/x/b_test.go", "c_test.go"},
			[]pair{{".", "go test ./ ./internal/x/"}}},
		{"angular", fakeTree{"angular.json": ""}, []string{"src/app/a.spec.ts"},
			[]pair{{".", "npx ng test --watch=false --include=src/app/a.spec.ts"}}},
		{"vitest", fakeTree{"package.json": `{"devDependencies":{"vitest":"1"}}`}, []string{"src/a.test.ts", "src/b.spec.tsx"},
			[]pair{{".", "npx vitest run src/a.test.ts src/b.spec.tsx"}}},
		{"jest", fakeTree{"package.json": `{"jest":{}}`}, []string{"src/a.test.js"},
			[]pair{{".", "npx jest src/a.test.js"}}},
		{"react-scripts", fakeTree{"package.json": `{"scripts":{"test":"react-scripts test"}}`}, []string{"src/App.test.jsx"},
			[]pair{{".", "npx react-scripts test --watchAll=false src/App.test.jsx"}}},
		{"maven-wrapper", fakeTree{"mvnw": "", "pom.xml": ""}, []string{"src/test/java/com/a/FooTest.java", "src/test/java/com/a/BarIT.java"},
			[]pair{{".", "./mvnw -q test -Dsurefire.failIfNoSpecifiedTests=false -Dtest=com.a.BarIT,com.a.FooTest"}}},
		{"maven", fakeTree{"pom.xml": ""}, []string{"src/test/java/com/a/FooTest.java"},
			[]pair{{".", "mvn -q test -Dsurefire.failIfNoSpecifiedTests=false -Dtest=com.a.FooTest"}}},
		{"gradle-wrapper", fakeTree{"gradlew": "", "build.gradle": ""}, []string{"src/test/java/com/a/FooTest.java", "src/test/kotlin/com/b/BarSpec.kt"},
			[]pair{{".", "./gradlew test --tests com.a.FooTest --tests com.b.BarSpec"}}},
		{"gradle", fakeTree{"build.gradle.kts": ""}, []string{"src/test/kotlin/com/a/FooTest.kt"},
			[]pair{{".", "gradle test --tests com.a.FooTest"}}},
		{"sbt", fakeTree{"build.sbt": ""}, []string{"src/test/scala/com/a/FooSpec.scala", "src/test/scala/com/a/BarTest.scala"},
			[]pair{{".", `sbt "testOnly com.a.BarTest com.a.FooSpec"`}}},
		{"pytest", fakeTree{"pyproject.toml": ""}, []string{"tests/test_a.py", "b_test.py"},
			[]pair{{".", "python -m pytest b_test.py tests/test_a.py"}}},
		{"spaces", fakeTree{"package.json": "vitest"}, []string{"src/my dir/a.test.ts"},
			[]pair{{".", `npx vitest run "src/my dir/a.test.ts"`}}},
	}
	for _, c := range cases {
		got, un := plain(rows, c.tests, c.tree)
		if !reflect.DeepEqual(got, c.want) || len(un) != 0 {
			t.Errorf("%s: got %v unmatched %v, want %v", c.name, got, un, c.want)
		}
	}
}

func TestMonorepo(t *testing.T) {
	tree := fakeTree{"go.mod": "", "web/package.json": `"vitest"`}
	got, un := plain(Builtin(), []string{"cmd/a_test.go", "web/src/a.spec.ts"}, tree)
	want := []pair{{".", "go test ./cmd/"}, {"web", "npx vitest run src/a.spec.ts"}}
	if !reflect.DeepEqual(got, want) || len(un) != 0 {
		t.Fatalf("got %v %v", got, un)
	}
}

func TestMavenMultiModule(t *testing.T) {
	tree := fakeTree{"pom.xml": "", "svc/pom.xml": "", "api/pom.xml": ""}
	got, _ := plain(Builtin(), []string{
		"svc/src/test/java/com/a/FooTest.java", "api/src/test/java/com/b/BarTest.java"}, tree)
	want := []pair{
		{"api", "mvn -q test -Dsurefire.failIfNoSpecifiedTests=false -Dtest=com.b.BarTest"},
		{"svc", "mvn -q test -Dsurefire.failIfNoSpecifiedTests=false -Dtest=com.a.FooTest"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
}

func TestMavenWrapperAtRoot(t *testing.T) {
	tree := fakeTree{"mvnw": "", "pom.xml": "", "svc/pom.xml": ""}
	got, un := plain(Builtin(), []string{"svc/src/test/java/com/a/FooTest.java"}, tree)
	want := []pair{{".", "./mvnw -q test -Dsurefire.failIfNoSpecifiedTests=false -Dtest=com.a.FooTest"}}
	if !reflect.DeepEqual(got, want) || len(un) != 0 {
		t.Fatalf("got %v %v", got, un)
	}
}

func TestGradleWrapperAtRoot(t *testing.T) {
	tree := fakeTree{"gradlew": "", "build.gradle": "", "svc/build.gradle": "", "api/impl/build.gradle.kts": ""}
	got, un := plain(Builtin(), []string{
		"svc/src/test/java/com/a/FooTest.java", "api/impl/src/test/kotlin/com/b/BarTest.kt"}, tree)
	want := []pair{
		{".", "./gradlew :api:impl:test --tests com.b.BarTest"},
		{".", "./gradlew :svc:test --tests com.a.FooTest"},
	}
	if !reflect.DeepEqual(got, want) || len(un) != 0 {
		t.Fatalf("got %v %v", got, un)
	}
}

func TestShellMetacharsUnmatched(t *testing.T) {
	tree := fakeTree{"package.json": "vitest"}
	bad := []string{"src/a;rm.test.ts", "src/$(x).test.ts", "src/a&b.test.ts", "src/a`b`.test.ts", "src/a\"b.test.ts", "src/a'b.test.ts", "src/a%b.test.ts"}
	got, un := plain(Builtin(), append([]string{"src/ok.test.ts"}, bad...), tree)
	want := []pair{{".", "npx vitest run src/ok.test.ts"}}
	if !reflect.DeepEqual(got, want) || len(un) != len(bad) {
		t.Fatalf("got %v %v", got, un)
	}
}

func TestByNameNeedsRowExtension(t *testing.T) {
	all := []string{"web/x.ts", "web/xTest.java", "web/test_x.py", "web/x.spec.ts"}
	got := ByName(Builtin(), []string{"web/x.ts"}, all)
	want := []string{"web/x.spec.ts"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestPlayLayout(t *testing.T) {
	got, _ := plain(Builtin(), []string{"test/controllers/HomeSpec.scala"}, fakeTree{"build.sbt": ""})
	want := []pair{{".", `sbt "testOnly controllers.HomeSpec"`}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
}

func TestUnmatched(t *testing.T) {
	got, un := plain(Builtin(), []string{"a_test.go", "z/b.spec.ts"}, fakeTree{})
	if len(got) != 0 || !reflect.DeepEqual(un, []string{"a_test.go", "z/b.spec.ts"}) {
		t.Fatalf("got %v %v", got, un)
	}
}

func TestDetectContains(t *testing.T) {
	// package.json without a known runner name: nothing matches.
	got, un := plain(Builtin(), []string{"a.test.ts"}, fakeTree{"package.json": `{"name":"x"}`})
	if len(got) != 0 || len(un) != 1 {
		t.Fatalf("got %v %v", got, un)
	}
}

func TestByName(t *testing.T) {
	rows := Builtin()
	all := []string{
		"a/foo.go", "a/foo_test.go", "b/foo_test.go",
		"web/x.ts", "web/x.spec.ts", "web/x.test.ts",
		"src/main/java/com/A.java", "src/test/java/com/ATest.java", "src/test/java/com/AIT.java",
		"p/mod.py", "p/test_mod.py", "p/other_test.py",
		"s/Foo.scala", "s/FooSpec.scala",
	}
	cases := []struct {
		changed []string
		want    []string
	}{
		{[]string{"a/foo.go"}, []string{"a/foo_test.go"}},
		{[]string{"web/x.ts"}, []string{"web/x.spec.ts", "web/x.test.ts"}},
		{[]string{"src/main/java/com/A.java"}, []string{"src/test/java/com/AIT.java", "src/test/java/com/ATest.java"}},
		{[]string{"p/mod.py"}, []string{"p/test_mod.py"}},
		{[]string{"s/Foo.scala"}, []string{"s/FooSpec.scala"}},
		{[]string{"b/foo_test.go"}, []string{"b/foo_test.go"}},
		{[]string{"README.md"}, []string{}},
	}
	for _, c := range cases {
		got := ByName(rows, c.changed, all)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%v: got %v want %v", c.changed, got, c.want)
		}
	}
}

func TestParseAndMerge(t *testing.T) {
	repo, err := Parse([]byte(`[{"name":"go","detect":[{"file":"go.mod"}],"tests":["*_test.go"],"command":"make test {tests}","item":"dir"}]`))
	if err != nil {
		t.Fatal(err)
	}
	rows := Merge(repo, Builtin())
	if rows[0].Command != "make test {tests}" {
		t.Fatalf("repo row not first: %v", rows[0])
	}
	n := 0
	for _, r := range rows {
		if r.Name == "go" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("go rows = %d", n)
	}
	got, _ := plain(rows, []string{"x/a_test.go"}, fakeTree{"go.mod": ""})
	if len(got) != 1 || got[0].Line != "make test ./x/" {
		t.Fatalf("got %v", got)
	}
}

func TestParseStrict(t *testing.T) {
	for _, bad := range []string{
		`[{"name":"x","oops":1}]`,
		`[{"name":"x","detect":[{"file":"f"}],"tests":["*"],"command":"no placeholder","item":"path"}]`,
		`[{"name":"x","detect":[{"file":"f"}],"tests":["*"],"command":"{tests}","item":"weird"}]`,
		`[{"name":"","detect":[{"file":"f"}],"tests":["*"],"command":"{tests}","item":"path"}]`,
	} {
		if _, err := Parse([]byte(bad)); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}

func TestIsTest(t *testing.T) {
	rows := Builtin()
	if !IsTest(rows, "a/b/foo_test.go") || IsTest(rows, "a/foo.go") {
		t.Fatal("IsTest wrong")
	}
}
