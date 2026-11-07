package legacy

// ConvertReviews reads the rows of the 1.x review.jsonl: the last "review" row of
// each id gives the findings offered, and a "chosen" row with that id and a
// finding marks it as chosen. repo is the repo the picks are counted for.
func ConvertReviews(rows []any, repo string) ([]Pick, Counts) {
	type review struct {
		ts       int64
		findings []any
	}
	reviews := map[string]review{}
	var order []string
	chosen := map[string]bool{}
	for _, r := range rows {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		id, _ := m["id"].(string)
		if id == "" {
			continue
		}
		switch m["kind"] {
		case "review":
			if _, seen := reviews[id]; !seen {
				order = append(order, id)
			}
			fs, _ := m["findings"].([]any)
			reviews[id] = review{ts: int64(num(m["ts"])), findings: fs}
		case "chosen":
			if f, _ := m["finding"].(string); f != "" {
				chosen[id+"\x00"+f] = true
			}
		}
	}
	var picks []Pick
	var c Counts
	for _, id := range order {
		rv := reviews[id]
		c.Reviews++
		for _, f := range rv.findings {
			fm, ok := f.(map[string]any)
			if !ok {
				continue
			}
			fid, _ := fm["id"].(string)
			if fid == "" {
				continue
			}
			lens, _ := fm["lens"].(string)
			sev, _ := fm["severity"].(string)
			p := Pick{
				FeatureID: "1x:" + id, Finding: fid, Repo: repo, Lens: lens, Severity: sev,
				Chosen: chosen[id+"\x00"+fid], CreatedAt: rv.ts,
			}
			picks = append(picks, p)
			c.Picks++
			if p.Chosen {
				c.Chosen++
			}
		}
	}
	return picks, c
}

func num(v any) float64 {
	f, _ := v.(float64)
	return f
}
