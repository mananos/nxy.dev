package jscompat

import (
	"encoding/hex"
	"regexp"
	"testing"

	"github.com/mananos/nxy.dev/internal/goldentest"
)

// SpaceClass must be exactly isSpace (the JS \s set) over the whole BMP.
func TestSpaceClassMatchesIsSpace(t *testing.T) {
	re := regexp.MustCompile("^[" + SpaceClass + "]$")
	for r := rune(0); r <= 0xFFFF; r++ {
		if r >= 0xD800 && r <= 0xDFFF {
			continue // surrogates are not valid Go runes
		}
		if got := re.MatchString(string(r)); got != isSpace(r) {
			t.Fatalf("U+%04X: SpaceClass %v, isSpace %v", r, got, isSpace(r))
		}
	}
	if re.MatchString(string(rune(0x1F600))) {
		t.Fatal("an astral character is not space")
	}
}

func TestGoldenTrimStart(t *testing.T) {
	forFile(t, "trimstart.json", func(t *testing.T, c goldentest.Case) {
		var s string
		c.In(t, "s", &s)
		goldentest.Check(t, c, map[string]any{"trimStart": TrimStart(s)})
	})
}

func TestGoldenUTF16Cmp(t *testing.T) {
	forFile(t, "utf16cmp.json", func(t *testing.T, c goldentest.Case) {
		var list []string
		c.In(t, "list", &list)
		var signs []int
		for _, a := range list {
			for _, b := range list {
				signs = append(signs, CompareUTF16(a, b))
			}
		}
		sorted := append([]string(nil), list...)
		sortUTF16(sorted)
		goldentest.Check(t, c, map[string]any{"signs": signs, "sorted": sorted})
	})
}

func TestGoldenUTF8Decode(t *testing.T) {
	forFile(t, "utf8decode.json", func(t *testing.T, c goldentest.Case) {
		var h string
		c.In(t, "hex", &h)
		b, err := hex.DecodeString(h)
		if err != nil {
			t.Fatal(err)
		}
		goldentest.Check(t, c, map[string]any{"text": DecodeUTF8(b)})
	})
}

func sortUTF16(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && CompareUTF16(s[j-1], s[j]) > 0; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
