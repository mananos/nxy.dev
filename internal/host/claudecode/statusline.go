package claudecode

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mananos/nxy.dev/v2/internal/host/claudecode/transcript"
)

// FoldStatusline folds the new transcript bytes into c (hosts/claude-code/statusline.mjs fold):
// the main transcript, then each subagent in name order with the parent's turn start. It returns
// the number of subagent transcripts seen.
func FoldStatusline(transcriptPath string, c *transcript.SessionCache) int {
	if c.Main == nil {
		c.Main = transcript.NewIncrementalState()
	}
	ReadIncremental(transcriptPath, c.Main, IncrementalOpts{})
	agentsDir := strings.TrimSuffix(transcriptPath, ".jsonl") + "/subagents"
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		return 0
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	count := 0
	for _, n := range names {
		if !strings.HasPrefix(n, "agent-") || !strings.HasSuffix(n, ".jsonl") {
			continue
		}
		count++
		st := c.Agents.Ensure(n)
		p := filepath.Join(agentsDir, n)
		info, err := os.Stat(p)
		if err != nil {
			continue // the agent file can vanish between readdir and stat
		}
		if info.Size() == st.Offset && equalTs(st.TurnStartTs, c.Main.TurnStartTs) {
			continue
		}
		ReadIncremental(p, st, IncrementalOpts{TurnSinceTs: c.Main.TurnStartTs})
		if st.LastTs != nil && (c.Main.LastTs == nil || *st.LastTs > *c.Main.LastTs) {
			v := *st.LastTs
			c.Main.LastTs = &v
		}
	}
	return count
}

func equalTs(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
