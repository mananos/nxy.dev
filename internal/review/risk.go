package review

import (
	"strings"

	"github.com/mananos/nxy.dev/internal/domain"
)

func rank(r domain.Risk) int {
	switch r {
	case domain.RiskHigh:
		return 2
	case domain.RiskMedium:
		return 1
	}
	return 0
}

// Max returns the highest of the risks (low when none).
func Max(rs ...domain.Risk) domain.Risk {
	out := domain.RiskLow
	for _, r := range rs {
		if rank(r) > rank(out) {
			out = r
		}
	}
	return out
}

// lensRisk is the risk a lens implies: security and database (migrations included) are high,
// api is medium, the rest imply nothing.
func lensRisk(name string) domain.Risk {
	switch name {
	case "security", "database":
		return domain.RiskHigh
	case "api", "contract":
		return domain.RiskMedium
	}
	return domain.RiskLow
}

// Raise lifts the planned risk by what the selected lenses show. It never lowers it. why says,
// in Spanish, which lenses and files raised it ("" when nothing was raised).
func Raise(planned domain.Risk, sel []Selected) (domain.Risk, string) {
	out := planned
	var why []string
	for _, s := range sel {
		r := lensRisk(s.Lens.Name)
		if rank(r) <= rank(planned) {
			continue
		}
		if rank(r) > rank(out) {
			out = r
		}
		files := s.Files
		if len(files) > 3 {
			files = files[:3]
		}
		why = append(why, "lente "+s.Lens.Name+": "+strings.Join(files, ", "))
	}
	return out, strings.Join(why, "; ")
}
