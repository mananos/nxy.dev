package platform

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/mananos/nxy.dev/internal/jscompat"
	"github.com/mananos/nxy.dev/internal/legacy"
)

// HeadBytes is how much of the start of a 1.x jsonl file the cursor hashes.
const HeadBytes = 4096

// LegacyMemoryDB is the 1.x memory database (core/memory/store.mjs): <user dir>/memory/memory.db.
func LegacyMemoryDB(getenv func(string) string) (string, error) {
	u, err := UserDirEnv(getenv)
	if err != nil {
		return "", err
	}
	return filepath.Join(u, "memory", "memory.db"), nil
}

// LegacyFilterLog is the 1.x filter summary log of the checkout that contains dir.
func LegacyFilterLog(dir string) string {
	return filepath.Join(RuntimeDir(dir), "metrics", "filter.jsonl")
}

// LegacyReviewLog is the 1.x review log of the checkout that contains dir.
func LegacyReviewLog(dir string) string {
	return filepath.Join(RuntimeDir(dir), "review.jsonl")
}

// FilterCheckout names the checkout in the filter tables: its runtime dir with forward slashes,
// in lower case on Windows (where two spellings of a path are the same folder).
func FilterCheckout(dir string) string {
	abs := NativePath(dir)
	if a, err := filepath.Abs(abs); err == nil {
		abs = a
	}
	s := filepath.ToSlash(RuntimeDir(abs))
	if runtime.GOOS == "windows" {
		s = strings.ToLower(s)
	}
	return s
}

var (
	reScheme = regexp.MustCompile(`(?i)^[a-z+]+://`)
	reUser   = regexp.MustCompile(`^[^@/]+@`)
	rePort   = regexp.MustCompile(`:\d+/`)
	reGitSfx = regexp.MustCompile(`(?i)\.git$`)
	reGitdir = regexp.MustCompile(`(?m)^gitdir:\s*(.+?)\s*$`)
	reRemote = regexp.MustCompile(`^\[remote\s+"([^"]+)"\]$`)
	reURL    = regexp.MustCompile(`^url\s*=\s*(.+)$`)
)

// legacyNormalizeRemote is normalizeRemote of core/memory/scope.mjs ("" is null).
func legacyNormalizeRemote(url string) string {
	s := jscompat.Trim(url)
	if s == "" {
		return ""
	}
	s = reGitSfx.ReplaceAllString(s, "")
	s = reScheme.ReplaceAllString(s, "")
	s = reUser.ReplaceAllString(s, "")
	// scp-style host:path -> host/path: the first ":" not followed by a digit.
	rs := []rune(s)
	for i := 0; i+1 < len(rs); i++ {
		if rs[i] == ':' && (rs[i+1] < '0' || rs[i+1] > '9') {
			rs[i] = '/'
			break
		}
	}
	s = string(rs)
	if loc := rePort.FindStringIndex(s); loc != nil {
		s = s[:loc[0]] + "/" + s[loc[1]:]
	}
	s = strings.TrimRight(s, "/")
	s = strings.ReplaceAll(s, `\`, "/")
	return strings.ToLower(s)
}

// legacyGitRemote is gitRemote of scope.mjs: the origin url, else the first remote in the config.
func legacyGitRemote(root string) string {
	dot := filepath.Join(root, ".git")
	gd := dot
	st, err := os.Stat(dot)
	if err != nil {
		return ""
	}
	if !st.IsDir() {
		b, err := os.ReadFile(dot)
		if err != nil {
			return ""
		}
		m := reGitdir.FindStringSubmatch(string(b))
		if m == nil {
			return ""
		}
		p := jscompat.Trim(m[1])
		if !filepath.IsAbs(p) {
			p = filepath.Join(root, p)
		}
		gd = filepath.Clean(p)
	}
	cfgDir := gd
	if _, err := os.Stat(filepath.Join(gd, "config")); err != nil {
		if b, err := os.ReadFile(filepath.Join(gd, "commondir")); err == nil {
			if c := jscompat.Trim(string(b)); c != "" {
				if !filepath.IsAbs(c) {
					c = filepath.Join(gd, c)
				}
				cfgDir = filepath.Clean(c)
			}
		}
	}
	text, err := os.ReadFile(filepath.Join(cfgDir, "config"))
	if err != nil {
		return ""
	}
	remotes := map[string]string{}
	var order []string
	current := ""
	in := false
	for _, raw := range strings.Split(string(text), "\n") {
		line := jscompat.Trim(raw)
		if m := reRemote.FindStringSubmatch(line); m != nil {
			current, in = m[1], true
			continue
		}
		if strings.HasPrefix(line, "[") {
			in = false
			continue
		}
		if m := reURL.FindStringSubmatch(line); in && m != nil {
			if _, seen := remotes[current]; !seen {
				order = append(order, current)
			}
			remotes[current] = jscompat.Trim(m[1])
		}
	}
	if u := remotes["origin"]; u != "" {
		return u
	}
	if len(order) > 0 {
		return remotes[order[0]]
	}
	return ""
}

// LegacyProjectKey is projectKey of core/memory/scope.mjs: the key the 1.x memory.db files a repo
// under. It is not RepoKey on purpose: the 1.x lower-cases everything and takes the first remote
// when there is no origin. With no remote it is "path:" and the lower-cased root.
func LegacyProjectKey(dir string) string {
	dir = NativePath(dir)
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	root, ok := GitRoot(dir)
	if ok {
		if n := legacyNormalizeRemote(legacyGitRemote(root)); n != "" {
			return n
		}
		return "path:" + strings.ToLower(filepath.ToSlash(root))
	}
	return "path:" + strings.ToLower(filepath.ToSlash(dir))
}

// FileHead hashes the first min(upTo, HeadBytes) bytes of a file; empty when it cannot be read.
func FileHead(path string, upTo int64) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	n := upTo
	if n > HeadBytes {
		n = HeadBytes
	}
	buf, _ := io.ReadAll(io.LimitReader(f, n))
	sum := sha256.Sum256(buf)
	return hex.EncodeToString(sum[:])
}

// FileSize is the size of a file and whether it exists.
func FileSize(path string) (int64, bool) {
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return 0, false
	}
	return st.Size(), true
}

// ReadJSONLFrom reads the parseable lines of a JSONL file from byte offset up to the last complete
// line: a line still being written (no trailing newline) is left for the next read. The cursor
// gives the offset after the last complete line, the size seen and the head hash up to that offset.
// An offset past the end of the file reads from the start.
func ReadJSONLFrom(path string, offset int64) ([]any, legacy.Cursor, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, legacy.Cursor{}, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, legacy.Cursor{}, err
	}
	if offset < 0 || offset > st.Size() {
		offset = 0
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, legacy.Cursor{}, err
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, legacy.Cursor{}, err
	}
	size := offset + int64(len(data))
	end := bytes.LastIndexByte(data, '\n') + 1
	next := legacy.Cursor{Offset: offset + int64(end), Size: size}
	var rows []any
	for _, line := range bytes.Split(data[:end], []byte("\n")) {
		if jscompat.Trim(string(line)) == "" {
			continue
		}
		var v any
		if json.Unmarshal(line, &v) != nil {
			continue
		}
		rows = append(rows, v)
	}
	next.Head = FileHead(path, next.Offset)
	return rows, next, nil
}
