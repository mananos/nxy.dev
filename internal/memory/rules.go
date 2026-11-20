package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"
)

// Fixed limits.
const (
	NoteMaxBytes   = 2048
	NoteTTL        = 30 * 24 * time.Hour
	ReviewAfter    = 90 * 24 * time.Hour
	DispatchBudget = 3000 // chars of rules delivered in one dispatch
	SearchLimit    = 20   // most hits a memory search returns
)

// ValidateRule checks a rule can be stored: a rule needs the question it settles.
func ValidateRule(r Rule) error {
	if strings.TrimSpace(r.Text) == "" {
		return errors.New("una regla necesita un texto")
	}
	if strings.TrimSpace(r.Question) == "" {
		return errors.New("una regla necesita la pregunta que resuelve")
	}
	valid := false
	for _, o := range Origins {
		if r.Origin == o {
			valid = true
		}
	}
	if !valid {
		return fmt.Errorf("origen de regla desconocido %q (uno de %s)", r.Origin, strings.Join(Origins, ", "))
	}
	for _, g := range r.Paths {
		if _, err := path.Match(g, ""); err != nil {
			return fmt.Errorf("glob de ruta inválido %q: %v", g, err)
		}
	}
	if len(r.Roles) > 0 && r.Repo != GlobalRepo {
		return errors.New("--roles sólo se aplica a reglas globales (G<n>)")
	}
	for _, role := range r.Roles {
		if !slices.Contains(RuleRoles, role) {
			return fmt.Errorf("rol desconocido %q (uno de %s)", role, strings.Join(RuleRoles, ", "))
		}
	}
	return nil
}

// CleanRoles trims, lowercases, dedupes and sorts roles.
func CleanRoles(in []string) []string {
	var out []string
	for _, r := range in {
		r = strings.ToLower(strings.TrimSpace(r))
		if r != "" && !slices.Contains(out, r) {
			out = append(out, r)
		}
	}
	sort.Strings(out)
	return out
}

// ValidateNote checks a note; an oversized body is rejected, not truncated.
func ValidateNote(n Note) error {
	if strings.TrimSpace(n.Title) == "" {
		return errors.New("una nota necesita un título")
	}
	if len(n.Body) > NoteMaxBytes {
		return fmt.Errorf("el cuerpo de la nota pesa %d bytes y el tope es %d", len(n.Body), NoteMaxBytes)
	}
	return nil
}

// NormalizeText lowercases, collapses whitespace and drops trailing punctuation.
func NormalizeText(s string) string {
	s = strings.Join(strings.Fields(strings.ToLower(s)), " ")
	return strings.TrimRightFunc(s, func(r rune) bool { return unicode.IsPunct(r) || unicode.IsSpace(r) })
}

// CleanPath returns a slash-separated path without "./" or redundant elements.
func CleanPath(p string) string {
	p = strings.ReplaceAll(strings.TrimSpace(p), `\`, "/")
	if p == "" {
		return ""
	}
	return path.Clean(p)
}

// LearningHash identifies a learning by its normalized text and its files.
func LearningHash(text string, files []string) string {
	fs := make([]string, 0, len(files))
	for _, f := range files {
		if c := CleanPath(f); c != "" {
			fs = append(fs, c)
		}
	}
	sort.Strings(fs)
	sum := sha256.Sum256([]byte(NormalizeText(text) + "\n" + strings.Join(fs, "\n")))
	return hex.EncodeToString(sum[:])
}

func folderKey(p string, foldCase bool) string {
	p = strings.TrimRight(strings.ReplaceAll(p, `\`, "/"), "/")
	if foldCase {
		p = strings.ToLower(p)
	}
	return p
}

// FolderMatch reports whether one folder contains the other, by segments (not
// by string prefix: /a/b does not match /a/bc).
func FolderMatch(a, b string, foldCase bool) bool {
	a, b = folderKey(a, foldCase), folderKey(b, foldCase)
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}

func ms(t int64) time.Time { return time.UnixMilli(t) }

// NoteExpired: 30 days without an update.
func NoteExpired(n Note, now time.Time) bool {
	return !now.Before(ms(n.UpdatedAt).Add(NoteTTL))
}

// NeedsReview: 90 days since the rule was created or last cited.
func NeedsReview(r Rule, now time.Time) bool {
	t := r.CreatedAt
	if r.LastCitedAt > t {
		t = r.LastCitedAt
	}
	return !now.Before(ms(t).Add(ReviewAfter))
}

// FTSQuery turns free text into an FTS5 query: every word quoted, AND-ed.
func FTSQuery(text string) string {
	words := strings.Fields(text)
	for i, w := range words {
		words[i] = `"` + strings.ReplaceAll(w, `"`, `""`) + `"`
	}
	return strings.Join(words, " AND ")
}

// PathMatches reports whether a stored path is the queried path or lives
// under the queried directory.
func PathMatches(stored, query string) bool {
	s, q := CleanPath(stored), CleanPath(query)
	if s == "" || q == "" {
		return false
	}
	return s == q || strings.HasPrefix(s, q+"/")
}
