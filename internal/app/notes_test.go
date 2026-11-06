package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mananos/nxy.dev/internal/memory"
)

type noteMemory struct {
	deliverMemory
	notes []memory.Note
	swept int
	// deletedFor are the feature ids whose notes were deleted.
	deletedFor []string
}

func (m *noteMemory) DeleteNotesOfFeature(_ context.Context, id string) (int, error) {
	m.deletedFor = append(m.deletedFor, id)
	return 0, nil
}

func (m *noteMemory) Notes(context.Context, string, bool, time.Time) ([]memory.Note, error) {
	return m.notes, nil
}
func (m *noteMemory) Note(_ context.Context, ref string) (memory.Note, error) {
	for _, n := range m.notes {
		if n.Short == ref {
			return n, nil
		}
	}
	return memory.Note{}, memory.ErrNotFound
}
func (m *noteMemory) SweepNotes(context.Context, time.Time) (int, error) { m.swept++; return 0, nil }

type fakeSessions struct {
	rows   map[string]SessionState
	pruned int
	recent *SessionState
}

func newFakeSessions() *fakeSessions { return &fakeSessions{rows: map[string]SessionState{}} }

func (f *fakeSessions) StartSession(_ context.Context, s SessionState) error {
	f.rows[s.ID] = s
	return nil
}
func (f *fakeSessions) Session(_ context.Context, id string) (SessionState, bool, error) {
	s, ok := f.rows[id]
	return s, ok, nil
}
func (f *fakeSessions) ConsumeNotesPending(_ context.Context, id string) (bool, error) {
	s, ok := f.rows[id]
	if !ok || !s.NotesPending {
		return false, nil
	}
	s.NotesPending = false
	f.rows[id] = s
	return true, nil
}
func (f *fakeSessions) ChainNote(_ context.Context, id, note string) error {
	s := f.rows[id]
	s.NoteID = note
	f.rows[id] = s
	return nil
}
func (f *fakeSessions) LinkFeature(_ context.Context, id, featureID string) error {
	s := f.rows[id]
	if s.ID == "" {
		s.ID = id
	}
	s.FeatureID = featureID
	f.rows[id] = s
	return nil
}
func (f *fakeSessions) OtherLive(_ context.Context, featureID, except string) (bool, error) {
	for id, s := range f.rows {
		if id != except && s.FeatureID == featureID && s.EndedAt == 0 {
			return true, nil
		}
	}
	return false, nil
}
func (f *fakeSessions) MarkEdit(_ context.Context, id, folder string, at int64) error {
	s := f.rows[id]
	if s.ID == "" {
		s.ID, s.Folder, s.StartedAt = id, folder, at
	}
	s.LastEditAt = at
	f.rows[id] = s
	return nil
}
func (f *fakeSessions) MarkStopAsked(_ context.Context, id string) error {
	s := f.rows[id]
	s.StopAsked = true
	f.rows[id] = s
	return nil
}
func (f *fakeSessions) MarkReadHint(context.Context, string, int64) (bool, error) {
	return true, nil
}
func (f *fakeSessions) EndSession(_ context.Context, id, reason string, at int64) error {
	s := f.rows[id]
	s.ID, s.EndReason, s.EndedAt = id, reason, at
	f.rows[id] = s
	return nil
}
func (f *fakeSessions) RecentEnd(context.Context, string, string, int64, bool) (SessionState, bool, error) {
	if f.recent == nil {
		return SessionState{}, false, nil
	}
	return *f.recent, true, nil
}
func (f *fakeSessions) PruneSessions(context.Context, time.Time) (int, error) {
	f.pruned++
	return 0, nil
}

func twoNotes() []memory.Note {
	now := time.Now().UnixMilli()
	return []memory.Note{
		{Meta: memory.Meta{UpdatedAt: now}, Short: "N1", Title: "migrar login", Body: "falta el test"},
		{Meta: memory.Meta{UpdatedAt: now}, Short: "N2", Title: "otra cosa"},
	}
}

func notesHooks(t *testing.T) (Hooks, *noteMemory, *fakeSessions) {
	t.Helper()
	h, _ := flowHooks(false)
	h.Exe = "/bin/nxy"
	mem := &noteMemory{notes: twoNotes()}
	ss := newFakeSessions()
	h.Memory, h.Sessions = mem, ss
	return h, mem, ss
}

func startEv(source, sid string) HookEvent {
	return HookEvent{Name: "SessionStart", Source: source, SessionID: sid, ProjectDir: "/p", Cwd: "/p"}
}

func promptEv(sid, p string) HookEvent {
	return HookEvent{Name: "UserPromptSubmit", Prompt: p, SessionID: sid, ProjectDir: "/p", Cwd: "/p"}
}

func TestSessionStartSweepsAndMarksNotesPending(t *testing.T) {
	h, mem, ss := notesHooks(t)
	h.Handle(context.Background(), startEv("startup", "s1"))
	if mem.swept != 1 || ss.pruned != 1 {
		t.Errorf("swept=%d pruned=%d", mem.swept, ss.pruned)
	}
	if s := ss.rows["s1"]; !s.NotesPending || s.Folder != "/p" {
		t.Errorf("row = %+v", s)
	}
	mem.notes = nil
	h.Handle(context.Background(), startEv("startup", "s2"))
	if ss.rows["s2"].NotesPending {
		t.Errorf("no notes, yet pending")
	}
}

func TestSessionStartWithActiveFeatureDoesNotAsk(t *testing.T) {
	h, _, ss := notesHooks(t)
	h.Handle(context.Background(), prompt("/nxy:feature algo"))
	h.Handle(context.Background(), startEv("startup", "s1"))
	if ss.rows["s1"].NotesPending {
		t.Errorf("pending with an active feature")
	}
}

func TestSessionStartClearRecommendsTheChainedNote(t *testing.T) {
	h, _, ss := notesHooks(t)
	ss.recent = &SessionState{ID: "old", NoteID: "N2", EndReason: "clear"}
	h.Handle(context.Background(), startEv("clear", "s1"))
	if got := ss.rows["s1"].RecommendedNote; got != "N2" {
		t.Errorf("recommended = %q", got)
	}
	h.Handle(context.Background(), startEv("startup", "s2"))
	if got := ss.rows["s2"].RecommendedNote; got != "" {
		t.Errorf("startup recommended = %q", got)
	}
}

func TestSessionStartResumeKeepsTheSessionState(t *testing.T) {
	h, _, ss := notesHooks(t)
	ss.rows["s1"] = SessionState{ID: "s1", NoteID: "N1", StopAsked: true}
	h.Handle(context.Background(), startEv("compact", "s1"))
	if s := ss.rows["s1"]; s.NoteID != "N1" || !s.StopAsked || s.NotesPending {
		t.Errorf("row = %+v", s)
	}
}

func TestSessionEndRecordsReason(t *testing.T) {
	h, _, ss := notesHooks(t)
	h.Handle(context.Background(), startEv("startup", "s1"))
	r := h.Handle(context.Background(), HookEvent{Name: "SessionEnd", SessionID: "s1", Reason: "clear"})
	if r.Ask != nil || r.Context != "" || r.Deny != "" {
		t.Errorf("response = %+v", r)
	}
	if s := ss.rows["s1"]; s.EndReason != "clear" || s.EndedAt == 0 {
		t.Errorf("row = %+v", s)
	}
}

func TestFirstPromptAsksOnceWithRecommendedFirst(t *testing.T) {
	h, _, ss := notesHooks(t)
	ss.recent = &SessionState{ID: "old", NoteID: "N2"}
	h.Handle(context.Background(), startEv("clear", "s1"))
	r := h.Handle(context.Background(), promptEv("s1", "seguimos"))
	if r.Ask == nil || r.Ask.Question != memory.NotesQuestionText {
		t.Fatalf("response = %+v", r)
	}
	if !strings.HasPrefix(r.Ask.Options[0].Label, "N2") || !strings.Contains(r.Ask.Options[0].Label, "(recomendada)") {
		t.Errorf("options = %+v", r.Ask.Options)
	}
	if r2 := h.Handle(context.Background(), promptEv("s1", "otra")); r2.Ask != nil {
		t.Errorf("asked twice: %+v", r2)
	}
}

func TestFirstPromptFeatureCommandConsumesWithoutAsking(t *testing.T) {
	h, _, ss := notesHooks(t)
	h.Handle(context.Background(), startEv("startup", "s1"))
	r := h.Handle(context.Background(), promptEv("s1", "/nxy:feature algo"))
	if r.Ask != nil || r.Next == nil {
		t.Errorf("response = %+v", r)
	}
	if ss.rows["s1"].NotesPending {
		t.Errorf("still pending")
	}
}

func TestFirstPromptOtherSlashCommandKeepsItPending(t *testing.T) {
	h, _, ss := notesHooks(t)
	h.Handle(context.Background(), startEv("startup", "s1"))
	if r := h.Handle(context.Background(), promptEv("s1", "/nxy:stats")); r.Ask != nil {
		t.Errorf("asked on a slash command: %+v", r)
	}
	if !ss.rows["s1"].NotesPending {
		t.Errorf("slash command used it up")
	}
	if r := h.Handle(context.Background(), promptEv("s1", "hola")); r.Ask == nil {
		t.Errorf("did not ask on the next prompt")
	}
}

func noteAnswerEv(sid, answer string) HookEvent {
	return HookEvent{Name: "PostToolUse", Tool: "AskUserQuestion", SessionID: sid, ProjectDir: "/p", Cwd: "/p",
		Answers: map[string]string{memory.NotesQuestionText: answer}}
}

func TestAnswerNotesChainsAndInjectsTheChosenNote(t *testing.T) {
	h, _, ss := notesHooks(t)
	h.Handle(context.Background(), startEv("startup", "s1"))
	r := h.Handle(context.Background(), noteAnswerEv("s1", "N1 · migrar login (hace 5 min)"))
	if ss.rows["s1"].NoteID != "N1" {
		t.Errorf("not chained: %+v", ss.rows["s1"])
	}
	for _, want := range []string{"N1", "migrar login", "falta el test", "note done N1"} {
		if !strings.Contains(r.Context, want) {
			t.Errorf("context lacks %q: %q", want, r.Context)
		}
	}
}

func TestAnswerNotesNoneInjectsNothing(t *testing.T) {
	h, _, ss := notesHooks(t)
	h.Handle(context.Background(), startEv("startup", "s1"))
	r := h.Handle(context.Background(), noteAnswerEv("s1", memory.NotesNone))
	if r.Context != "" || r.Next != nil || ss.rows["s1"].NoteID != "" {
		t.Errorf("response = %+v row = %+v", r, ss.rows["s1"])
	}
}

func TestAnswerNotesWorksWithoutAFeature(t *testing.T) {
	// the generic AskUserQuestion branch leaves when there is no active feature: notes come first
	h, _, _ := notesHooks(t)
	r := h.Handle(context.Background(), noteAnswerEv("s9", "N2 · otra cosa (hace 1 h)"))
	if !strings.Contains(r.Context, "otra cosa") {
		t.Errorf("context = %q", r.Context)
	}
}

func TestAnswerNotesMissingNoteSaysNothing(t *testing.T) {
	h, mem, _ := notesHooks(t)
	mem.notes = nil
	if r := h.Handle(context.Background(), noteAnswerEv("s1", "N1 · x (hace 1 h)")); r.Context != "" {
		t.Errorf("context = %q", r.Context)
	}
}
