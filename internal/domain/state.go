package domain

// Phase is the internal step of a feature.
type Phase string

const (
	PhaseNone               Phase = ""
	PhaseDesigning          Phase = "designing"
	PhasePlanning           Phase = "planning"
	PhaseQuestions          Phase = "questions"
	PhaseNoTests            Phase = "no_tests"
	PhaseApproval           Phase = "approval"
	PhaseChangeNote         Phase = "change_note"
	PhasePlannerFailed      Phase = "planner_failed"
	PhaseRunningBatch       Phase = "running_batch"
	PhaseVerifyingBatch     Phase = "verifying_batch"
	PhaseBatchFailed        Phase = "batch_failed"
	PhaseAffected           Phase = "affected"
	PhaseAffectedFix        Phase = "affected_fix"
	PhaseAffectedFailed     Phase = "affected_failed"
	PhaseAffectedUnverified Phase = "affected_unverified"
	PhaseReviewing          Phase = "reviewing"
	PhaseRefuting           Phase = "refuting"
	PhaseChoosingFindings   Phase = "choosing_findings"
	PhaseFixing             Phase = "fixing"
	PhaseFixVerify          Phase = "fix_verify"
	PhaseFixesFailed        Phase = "fixes_failed"
	PhaseDone               Phase = "done"
)

// Phases lists every internal phase (not the empty initial one).
var Phases = []Phase{
	PhaseDesigning, PhasePlanning, PhaseQuestions, PhaseNoTests, PhaseApproval, PhaseChangeNote,
	PhasePlannerFailed, PhaseRunningBatch, PhaseVerifyingBatch, PhaseBatchFailed,
	PhaseAffected, PhaseAffectedFix, PhaseAffectedFailed, PhaseAffectedUnverified, PhaseReviewing,
	PhaseRefuting, PhaseChoosingFindings, PhaseFixing,
	PhaseFixVerify, PhaseFixesFailed, PhaseDone,
}

// Public is the status the user sees.
type Public string

const (
	Working   Public = "Trabajando"
	Verifying Public = "Verificando"
	NeedsYou  Public = "Necesita tu decisión"
	Finished  Public = "Listo"
)

type phaseInfo struct {
	public Public
	label  string
}

var phaseTable = map[Phase]phaseInfo{
	PhaseDesigning:          {NeedsYou, "Diseñando"},
	PhasePlanning:           {Working, "Planificando"},
	PhaseQuestions:          {NeedsYou, "Preguntas del plan"},
	PhaseNoTests:            {NeedsYou, "Sin tests"},
	PhaseApproval:           {NeedsYou, "Aprobación del plan"},
	PhaseChangeNote:         {NeedsYou, "Nota de cambio"},
	PhasePlannerFailed:      {NeedsYou, "El planner falló"},
	PhaseRunningBatch:       {Working, "Ejecutando lote"},
	PhaseVerifyingBatch:     {Verifying, "Verificando lote"},
	PhaseBatchFailed:        {NeedsYou, "Lote fallido"},
	PhaseAffected:           {Verifying, "Tests afectados"},
	PhaseAffectedFix:        {Working, "Arreglando afectados"},
	PhaseAffectedFailed:     {NeedsYou, "Afectados fallidos"},
	PhaseAffectedUnverified: {NeedsYou, "Afectados sin verificar"},
	PhaseReviewing:          {Verifying, "Revisando"},
	PhaseRefuting:           {Verifying, "Refutando hallazgos"},
	PhaseChoosingFindings:   {NeedsYou, "Elegir hallazgos"},
	PhaseFixing:             {Working, "Arreglando hallazgos"},
	PhaseFixVerify:          {Verifying, "Verificando arreglos"},
	PhaseFixesFailed:        {NeedsYou, "Arreglos fallidos"},
	PhaseDone:               {Finished, "Listo"},
}

// Public maps the internal phase to the user-facing status.
func (p Phase) Public() Public { return phaseTable[p].public }

// Label is the short Spanish name of the phase.
func (p Phase) Label() string { return phaseTable[p].label }

// Outcome says how a done feature ended.
type Outcome string

const (
	OutcomeNone    Outcome = ""
	OutcomeSuccess Outcome = "success"
	OutcomeStopped Outcome = "stopped"
)

var outcomeLabels = map[Outcome]string{
	OutcomeSuccess: "Listo",
	OutcomeStopped: "Parada",
}

// Label is the short Spanish name of the outcome.
func (o Outcome) Label() string { return outcomeLabels[o] }

// State is where a feature is in the flow.
type State struct {
	Phase   Phase   `json:"phase"`
	Batch   int     `json:"batch,omitempty"`   // current batch, 1-based
	Batches int     `json:"batches,omitempty"` // total batches in the approved plan
	Attempt int     `json:"attempt,omitempty"` // retries used in the current step
	Outcome Outcome `json:"outcome,omitempty"`
}
