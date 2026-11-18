package tools

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/mananos/nxy.dev/v2/internal/docs"
	"github.com/mananos/nxy.dev/v2/internal/domain"
	"github.com/mananos/nxy.dev/v2/internal/jscompat"
)

// skipDirs are the folders the docs search never enters: dependencies, builds and tool state.
var skipDirs = []string{"node_modules", ".git", ".nxy", "target", "build", "dist", "out", ".next", ".angular", "vendor", ".venv", "venv", "__pycache__", "coverage", ".gradle", ".idea", ".vscode"}

const (
	docsMaxFiles = 5000
	docsMaxBytes = 1024 * 1024
)

func isSkipDir(name string) bool {
	for _, d := range skipDirs {
		if d == name {
			return true
		}
	}
	return false
}

var (
	pipelineName = regexp.MustCompile(`(?i)^(azure-pipelines.*|\.gitlab-ci|bitbucket-pipelines)\.ya?ml$`)
	yamlName     = regexp.MustCompile(`(?i)\.ya?ml$`)
	docLineSplit = regexp.MustCompile(`\r?\n`)
)

// pipelineTexts reads the CI pipeline files that may publish docs to a wiki.
func pipelineTexts(root string) []string {
	var files []string
	addDir := func(dir string, name *regexp.Regexp) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			if name.MatchString(e.Name()) {
				files = append(files, filepath.Join(dir, e.Name()))
			}
		}
	}
	addDir(filepath.Join(root, ".github", "workflows"), yamlName)
	addDir(root, pipelineName)
	addDir(filepath.Join(root, ".azure-pipelines"), yamlName)
	out := make([]string, 0, len(files))
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			out = append(out, "")
			continue
		}
		out = append(out, jscompat.DecodeUTF8(b))
	}
	return out
}

// walkMd lists every .md/.mdx under dir, skipping dependency and build folders, up to the cap.
func walkMd(dir string, out *[]string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if len(*out) >= docsMaxFiles {
			return
		}
		if e.IsDir() {
			if !isSkipDir(e.Name()) {
				walkMd(filepath.Join(dir, e.Name()), out)
			}
		} else if n := strings.ToLower(e.Name()); strings.HasSuffix(n, ".md") || strings.HasSuffix(n, ".mdx") {
			*out = append(*out, filepath.Join(dir, e.Name()))
		}
	}
}

// FindDocs finds the docs of the repo at root that name what the changed files touched
// (repo-relative paths, forward slashes), with rg when it is there.
func FindDocs(ctx context.Context, root string, changed []string) domain.Docs {
	bin, _ := FindRg(os.Getenv)
	return findDocs(ctx, root, changed, bin)
}

// findDocs is FindDocs with the rg to use; "" walks the files (what happens without rg).
//
// With rg: one search over user-facing docs only (README, root `.md`, `docs/` `doc/` `documentation/`
// `wiki/` and the CI wiki folder), respecting .gitignore: milliseconds. Without it: a walk over the
// repo's `.md` files that skips dependency and build folders, reading each once (5000 files and 1 MB
// each at most). It runs once per feature, never per edit.
func findDocs(ctx context.Context, root string, changed []string, rgBin string) domain.Docs {
	terms := docs.DocTerms(changed)
	isDir := func(rel string) bool {
		fi, err := os.Stat(filepath.Join(root, rel))
		return err == nil && fi.IsDir()
	}
	wikiDir := docs.WikiDirFromCi(pipelineTexts(root), isDir)
	res := domain.Docs{Terms: terms, WikiDir: wikiDir}
	if len(terms) == 0 {
		return res
	}
	already := map[string]bool{}
	for _, p := range changed {
		already[p] = true
	}
	hits := map[string][]domain.DocLine{}
	counts := map[string]int{}
	var order []string
	add := func(path string, line int, text string) {
		if already[path] || !docs.IsUserDoc(path, wikiDir) {
			return
		}
		if counts[path] == 0 {
			order = append(order, path)
		}
		counts[path]++
		if len(hits[path]) < 3 {
			hits[path] = append(hits[path], domain.DocLine{Line: line, Text: jscompat.UTF16Prefix(jscompat.Trim(text), 160)})
		}
	}
	pattern := docs.TermsPattern(terms)
	if rgBin != "" {
		// The same folders the walk skips: .gitignore does not always list them (or there is no repo).
		globs := docs.UserDocGlobs(wikiDir)
		for _, d := range skipDirs {
			globs = append(globs, "!"+d)
		}
		r := RgSearch(ctx, root, pattern, RgOpts{Bin: rgBin, Regex: true, Globs: globs, MaxRows: 300})
		for _, row := range r.Rows {
			add(row.Path, row.Line, row.Text)
		}
	} else if re, err := regexp.Compile(pattern); err == nil {
		var files []string
		walkMd(root, &files)
		for _, file := range files {
			fi, err := os.Stat(file)
			if err != nil || fi.Size() > docsMaxBytes {
				continue
			}
			b, err := os.ReadFile(file)
			if err != nil {
				continue
			}
			rel, err := filepath.Rel(root, file)
			if err != nil {
				continue
			}
			rel = filepath.ToSlash(rel)
			for i, text := range docLineSplit.Split(jscompat.DecodeUTF8(b), -1) {
				if re.MatchString(text) {
					add(rel, i+1, text)
				}
			}
		}
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := order[i], order[j]
		if counts[a] != counts[b] {
			return counts[a] > counts[b]
		}
		return jscompat.LocaleCompare(a, b) < 0
	})
	for _, p := range order {
		res.Docs = append(res.Docs, domain.DocFile{Path: p, Lines: hits[p]})
	}
	return res
}
