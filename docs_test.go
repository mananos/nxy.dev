package nxydev

import (
	"image/png"
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

// pngSize returns the canvas of a PNG without decoding the pixels.
func pngSize(t *testing.T, f string) (int, int, bool) {
	t.Helper()
	fh, err := os.Open(f)
	if err != nil {
		t.Errorf("%s: %v", f, err)
		return 0, 0, false
	}
	defer fh.Close()
	cfg, err := png.DecodeConfig(fh)
	if err != nil {
		t.Errorf("%s: %v", f, err)
		return 0, 0, false
	}
	return cfg.Width, cfg.Height, true
}

// checkCaptures: every row of capturas.tsv has its PNG (and the other way round), every capture is
// 2560 wide and the banner is 2560x1440.
func checkCaptures(t *testing.T, assets string) {
	t.Helper()
	dir := filepath.Join(assets, "capturas")
	raw, err := os.ReadFile(filepath.Join(dir, "capturas.tsv"))
	if err != nil {
		t.Errorf("capturas.tsv: %v", err)
		return
	}
	rows := map[string]bool{}
	for i, line := range strings.Split(strings.ReplaceAll(string(raw), "\r", ""), "\n") {
		if i == 0 || strings.TrimSpace(line) == "" {
			continue
		}
		rows[strings.Split(line, "\t")[0]] = true
	}
	for n := range rows {
		f := filepath.Join(dir, n+".png")
		if !nonEmpty(f) {
			t.Errorf("capturas.tsv row %q has no non-empty %s", n, f)
		}
	}
	for n := range listNames(t, dir, ".png") {
		f := filepath.Join(dir, n+".png")
		if !rows[n] {
			t.Errorf("orphan %s: no row in capturas.tsv", f)
		}
		if w, _, ok := pngSize(t, f); ok && w != 2560 {
			t.Errorf("%s is %d wide, want 2560", f, w)
		}
	}
	banner := filepath.Join(assets, "brand", "banner.png")
	if w, h, ok := pngSize(t, banner); ok && (w != 2560 || h != 1440) {
		t.Errorf("%s is %dx%d, want 2560x1440", banner, w, h)
	}
}

func TestDocAssets(t *testing.T) {
	assets := filepath.Join("docs", "assets")
	checkCaptures(t, assets)
	checkPairs(t, filepath.Join(assets, "diagrams"), ".html", filepath.Join(assets, "diagrams"), ".png")
	for n := range listNames(t, filepath.Join(assets, "diagrams"), ".png") {
		f := filepath.Join(assets, "diagrams", n+".png")
		if w, h, ok := pngSize(t, f); ok && (w != 2560 || h != 1440) {
			t.Errorf("%s is %dx%d, want 2560x1440", f, w, h)
		}
	}

	videoDir := filepath.Join(assets, "video")
	entries, err := os.ReadDir(videoDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "node_modules" {
			continue
		}
		if !nonEmpty(filepath.Join(videoDir, e.Name(), "index.html")) {
			t.Errorf("%s has no index.html", filepath.Join(videoDir, e.Name()))
		}
		if mp4 := filepath.Join(videoDir, e.Name()+".mp4"); !nonEmpty(mp4) {
			t.Errorf("%s has no rendered %s", filepath.Join(videoDir, e.Name()), mp4)
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
