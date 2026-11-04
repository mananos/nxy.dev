package app

import (
	"context"
	"runtime"
	"strings"
	"time"

	"github.com/mananos/nxy.dev/internal/memory"
)

// foldNoteFolders compares note folders ignoring case on Windows.
var foldNoteFolders = runtime.GOOS == "windows"

const (
	// sessionRetention is how long a session row is kept after its last update.
	sessionRetention = 7 * 24 * time.Hour
	// clearPairWindow is how close a SessionStart(clear) must be to a SessionEnd(clear) to be the same /clear.
	clearPairWindow = 2 * time.Second
)

func (h Hooks) now() time.Time {
	if h.Clock != nil {
		return h.Clock.Now()
	}
	return time.Now()
}

// sessionFolder is the folder a session's notes belong to: the cwd, then the project dir.
func sessionFolder(ev HookEvent) string {
	if ev.Cwd != "" {
		return ev.Cwd
	}
	return ev.ProjectDir
}

// startNotesSession runs at SessionStart: it sweeps old notes and sessions, and stores the session
// row with the notes question pending when the folder has notes and no feature is active (with a
// feature the notes go in its resume summary).
func (h Hooks) startNotesSession(ctx context.Context, ev HookEvent, featureActive bool) {
	if h.Sessions == nil || h.Memory == nil || ev.SessionID == "" {
		return
	}
	now := h.now()
	_, _ = h.Memory.SweepNotes(ctx, now)
	_, _ = h.Sessions.PruneSessions(ctx, now.Add(-sessionRetention))
	folder := sessionFolder(ev)
	s := SessionState{ID: ev.SessionID, Folder: folder, Source: ev.Source, StartedAt: now.UnixMilli()}
	if prev, ok, err := h.Sessions.Session(ctx, ev.SessionID); err == nil && ok && (ev.Source == "resume" || ev.Source == "compact") {
		// the same session continues: keep what it already did
		s = prev
		s.Folder, s.Source = folder, ev.Source
	} else {
		if !featureActive {
			notes, err := h.Memory.Notes(ctx, folder, foldNoteFolders, now)
			s.NotesPending = err == nil && len(notes) > 0
		}
		if ev.Source == "clear" {
			since := now.Add(-clearPairWindow).UnixMilli()
			if old, ok, err := h.Sessions.RecentEnd(ctx, folder, "clear", since, foldNoteFolders); err == nil && ok && old.ID != ev.SessionID {
				s.RecommendedNote = old.NoteID
			}
		}
	}
	_ = h.Sessions.StartSession(ctx, s)
}

// sessionEnd handles SessionEnd: it records how the session ended.
func (h Hooks) sessionEnd(ctx context.Context, ev HookEvent) HookResponse {
	if h.Sessions != nil && ev.SessionID != "" {
		_ = h.Sessions.EndSession(ctx, ev.SessionID, ev.Reason, h.now().UnixMilli())
	}
	return HookResponse{}
}

// markEdit records that the session edited something (a file edit or a shell write that was not
// stopped), so the Stop knows whether there is work worth a note.
func (h Hooks) markEdit(ctx context.Context, ev HookEvent) {
	if h.Sessions == nil || ev.SessionID == "" {
		return
	}
	_ = h.Sessions.MarkEdit(ctx, ev.SessionID, sessionFolder(ev), h.now().UnixMilli())
}

// stopNote asks, once per session, for a note when the main thread is about to stop: no feature
// is active, the session edited something and no note written after that edit exists. ok is false
// when the Stop goes on.
func (h Hooks) stopNote(ctx context.Context, ev HookEvent) (HookResponse, bool) {
	if h.Sessions == nil || h.Memory == nil || ev.SessionID == "" || ev.AgentID != "" {
		return HookResponse{}, false
	}
	if _, active := h.activeFeature(ctx, ev); active {
		return HookResponse{}, false
	}
	s, found, err := h.Sessions.Session(ctx, ev.SessionID)
	if err != nil || !found || s.StopAsked || s.LastEditAt == 0 {
		return HookResponse{}, false
	}
	since := s.StartedAt
	if s.NoteID != "" {
		if n, err := h.Memory.Note(ctx, s.NoteID); err == nil && !n.Deleted() && n.UpdatedAt > since {
			since = n.UpdatedAt
		}
	}
	if s.LastEditAt <= since {
		return HookResponse{}, false
	}
	if err := h.Sessions.MarkStopAsked(ctx, ev.SessionID); err != nil {
		return HookResponse{}, false
	}
	exe := h.Exe
	if exe == "" {
		exe = "nxy"
	}
	id := ""
	if s.NoteID != "" {
		id = " --id " + s.NoteID
	}
	var b strings.Builder
	b.WriteString("nxy: before you stop, save a short note so the next session can pick this up. Run:\n\n")
	b.WriteString("\"" + exe + "\" note save \"<short title>\" --session " + ev.SessionID + id + " <<'NOTE'\n")
	b.WriteString("<what was done, what is missing, the next step; 2 KB at most>\nNOTE\n\n")
	b.WriteString("(PowerShell: pipe a here-string into `& \"" + exe + "\" note save ...` with the same arguments.)\n")
	if s.NoteID != "" {
		b.WriteString("This session continues note " + s.NoteID + ": the command above updates it.\n")
	}
	b.WriteString("If there is nothing worth saving, say so in one line and do not run it. Do not ask the user.")
	return HookResponse{Block: b.String()}, true
}

// firstPromptNotes handles the first prompt of a session that has notes: it asks which one to
// continue. `/nxy:feature` and `/nxy:engineer` use up the question without asking (the feature
// starts); any other slash command leaves it for the next prompt. ok is false when the prompt goes on.
func (h Hooks) firstPromptNotes(ctx context.Context, ev HookEvent) (HookResponse, bool) {
	if h.Sessions == nil || h.Memory == nil || ev.SessionID == "" || ev.AgentID != "" {
		return HookResponse{}, false
	}
	s, found, err := h.Sessions.Session(ctx, ev.SessionID)
	if err != nil || !found || !s.NotesPending {
		return HookResponse{}, false
	}
	_, isFeature := featureRequest(ev.Prompt)
	if !isFeature {
		_, isFeature = commandRequest(ev.Prompt, engineerCommand)
	}
	if !isFeature && strings.HasPrefix(strings.TrimSpace(ev.Prompt), "/") {
		return HookResponse{}, false
	}
	if got, err := h.Sessions.ConsumeNotesPending(ctx, ev.SessionID); err != nil || !got || isFeature {
		return HookResponse{}, false
	}
	now := h.now()
	notes, err := h.Memory.Notes(ctx, s.Folder, foldNoteFolders, now)
	if err != nil || len(notes) == 0 {
		return HookResponse{}, false
	}
	q := memory.NotesQuestion(notes, s.RecommendedNote, now)
	spec := &AskSpec{Question: q.Question, Header: q.Header}
	for _, o := range q.Options {
		spec.Options = append(spec.Options, AskOption{Label: o.Label, Description: o.Description})
	}
	return HookResponse{Ask: spec}, true
}

// answerNotes turns the answer to the notes question into context: the chosen note is chained to the
// session and its text goes to the main thread; "none" injects nothing. ok is false when the
// answers are not to that question.
func (h Hooks) answerNotes(ctx context.Context, ev HookEvent) (HookResponse, bool) {
	answer, found := ev.Answers[memory.NotesQuestionText]
	if !found {
		return HookResponse{}, false
	}
	if h.Memory == nil {
		return HookResponse{}, true
	}
	ref := memory.NoteChoice(answer)
	if ref == "" {
		return HookResponse{}, true
	}
	n, err := h.Memory.Note(ctx, ref)
	if err != nil || n.Deleted() {
		return HookResponse{}, true
	}
	if h.Sessions != nil && ev.SessionID != "" {
		_ = h.Sessions.ChainNote(ctx, ev.SessionID, ref)
	}
	exe := h.Exe
	if exe == "" {
		exe = "nxy"
	}
	var b strings.Builder
	b.WriteString("nxy: el usuario sigue con la nota " + ref + " «" + n.Title + "»")
	if body := strings.TrimSpace(n.Body); body != "" {
		b.WriteString(":\n" + body)
	}
	b.WriteString("\nAl terminar, ofrecé `\"" + exe + "\" note done " + ref + "` para borrarla.")
	return HookResponse{Context: b.String()}, true
}
