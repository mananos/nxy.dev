package claudecode

import (
	"io"
	"os"

	"github.com/mananos/nxy.dev/internal/host/claudecode/transcript"
)

// contextTailBytes is how much of the end of the transcript is read to measure the main context.
const contextTailBytes = 256 << 10

// MainContextTokens is the size of the main thread's context at its last assistant turn, read from
// the tail of the transcript. ok is false when it cannot be known (no path, unreadable file, no
// usage in the tail): the gate fails open.
func MainContextTokens(transcriptPath string) (tokens float64, ok bool) {
	if transcriptPath == "" {
		return 0, false
	}
	f, err := os.Open(transcriptPath)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return 0, false
	}
	size, truncated := st.Size(), false
	if size > contextTailBytes {
		if _, err := f.Seek(size-contextTailBytes, io.SeekStart); err != nil {
			return 0, false
		}
		truncated = true
	}
	tail, err := io.ReadAll(f)
	if err != nil {
		return 0, false
	}
	return transcript.LastContextTokens(tail, truncated)
}
