package nxydev

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var (
	reFence      = regexp.MustCompile("(?s)```.*?```|~~~.*?~~~")
	reInlineCode = regexp.MustCompile("`[^`\n]*`")
	reComment    = regexp.MustCompile(`(?s)<!--.*?-->`)
	reMdLink     = regexp.MustCompile(`!?\[[^\]]*\]\(\s*<?([^)\s>]+)>?(?:\s+"[^"]*")?\s*\)`)
	reHTMLAttr   = regexp.MustCompile(`(?i)\b(?:src|href)\s*=\s*["']([^"']+)["']`)
	reSlashCmd   = regexp.MustCompile(`/nxy:([a-z][a-z0-9-]*)`)
)

// docFiles returns README.md plus the top-level docs/*.md, minus the
// gitignored local-only roadmap-debate.md.
func docFiles(t *testing.T) []string {
	t.Helper()
	files := []string{"README.md"}
	matches, err := filepath.Glob(filepath.Join("docs", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range matches {
		if filepath.Base(m) == "roadmap-debate.md" {
			continue
		}
		files = append(files, m)
	}
	sort.Strings(files)
	return files
}

// blank replaces every match with the same number of newlines so line
// numbers stay valid.
func blank(re *regexp.Regexp, s string) string {
	return re.ReplaceAllStringFunc(s, func(m string) string {
		return strings.Repeat("\n", strings.Count(m, "\n"))
	})
}

func stripCode(s string) string {
	s = blank(reComment, s)
	s = blank(reFence, s)
	return blank(reInlineCode, s)
}

func TestDocLinks(t *testing.T) {
	for _, f := range docFiles(t) {
		raw, err := os.ReadFile(f)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			t.Fatal(err)
		}
		dir := filepath.Dir(f)
		for i, line := range strings.Split(stripCode(string(raw)), "\n") {
			var targets []string
			for _, m := range reMdLink.FindAllStringSubmatch(line, -1) {
				targets = append(targets, m[1])
			}
			for _, m := range reHTMLAttr.FindAllStringSubmatch(line, -1) {
				targets = append(targets, m[1])
			}
			for _, tg := range targets {
				low := strings.ToLower(tg)
				if strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "https://") ||
					strings.HasPrefix(low, "mailto:") || strings.HasPrefix(tg, "#") {
					continue
				}
				if j := strings.Index(tg, "#"); j >= 0 {
					tg = tg[:j]
				}
				if tg == "" {
					continue
				}
				if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(tg))); err != nil {
					t.Errorf("%s:%d -> %s", f, i+1, tg)
				}
			}
		}
	}
}

func listNames(t *testing.T, dir, ext string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ext) {
			out[strings.TrimSuffix(e.Name(), ext)] = true
		}
	}
	return out
}

func nonEmpty(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir() && st.Size() > 0
}

func checkPairs(t *testing.T, srcDir, srcExt, dstDir, dstExt string) {
	t.Helper()
	src := listNames(t, srcDir, srcExt)
	dst := listNames(t, dstDir, dstExt)
	for n := range src {
		if !nonEmpty(filepath.Join(dstDir, n+dstExt)) {
			t.Errorf("%s has no non-empty %s", filepath.Join(srcDir, n+srcExt), filepath.Join(dstDir, n+dstExt))
		}
	}
	for n := range dst {
		if !src[n] {
			t.Errorf("orphan %s: no %s", filepath.Join(dstDir, n+dstExt), filepath.Join(srcDir, n+srcExt))
		}
	}
}

func TestDocAssets(t *testing.T) {
	assets := filepath.Join("docs", "assets")
	checkPairs(t, filepath.Join(assets, "tapes"), ".tape", filepath.Join(assets, "gifs"), ".gif")
	checkPairs(t, filepath.Join(assets, "diagrams"), ".d2", filepath.Join(assets, "diagrams"), ".svg")

	videoDir := filepath.Join(assets, "video")
	entries, err := os.ReadDir(videoDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "out" || e.Name() == "node_modules" {
			continue
		}
		if !nonEmpty(filepath.Join(videoDir, e.Name(), "index.html")) {
			t.Errorf("%s has no index.html", filepath.Join(videoDir, e.Name()))
		}
	}
}

func TestDocSlashCommands(t *testing.T) {
	for _, f := range docFiles(t) {
		raw, err := os.ReadFile(f)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(raw), "\n") {
			for _, m := range reSlashCmd.FindAllStringSubmatch(line, -1) {
				if _, err := os.Stat(filepath.Join("commands", m[1]+".md")); err != nil {
					t.Errorf("%s:%d -> /nxy:%s (no commands/%s.md)", f, i+1, m[1], m[1])
				}
			}
		}
	}
}
