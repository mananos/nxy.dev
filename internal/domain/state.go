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
	PhaseRepoClash          Phase = "repo_clash"
	PhaseBranch             Phase = "branch"
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
	PhaseDocsCheck          Phase = "docs_check"
	PhaseDocs               Phase = "docs"
	PhaseDocumenting        Phase = "documenting"
	PhaseRules              Phase = "rules"
	PhaseDone               Phase = "done"
)

// Phases lists every internal phase (not the empty initial one).
var Phases = []Phase{
	PhaseDesigning, PhasePlanning, PhaseQuestions, PhaseNoTests, PhaseApproval, PhaseChangeNote,
	PhasePlannerFailed, PhaseRepoClash, PhaseBranch, PhaseRunningBatch, PhaseVerifyingBatch, PhaseBatchFailed,
	PhaseAffected, PhaseAffectedFix, PhaseAffectedFailed, PhaseAffectedUnverified, PhaseReviewing,
	PhaseRefuting, PhaseChoosingFindings, PhaseFixing,
	PhaseFixVerify, PhaseFixesFailed, PhaseDocsCheck, PhaseDocs, PhaseDocumenting, PhaseRules, PhaseDone,
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
	PhaseRepoClash:          {NeedsYou, "Repo ocupado"},
	PhaseBranch:             {NeedsYou, "Rama"},
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
	PhaseDocsCheck:          {Verifying, "Buscando docs"},
	PhaseDocs:               {NeedsYou, "Actualizar docs"},
	PhaseDocumenting:        {Working, "Actualizando docs"},
	PhaseRules:              {NeedsYou, "Reglas del repo"},
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
	Batch   int     `json:"batch,omitempty"`   // batch in focus: the current one in a row, the asked one in batch_failed
	Batches int     `json:"batches,omitempty"` // total batches in the approved plan
	Attempt int     `json:"attempt,omitempty"` // retries used in the current step
	Outcome Outcome `json:"outcome,omitempty"`
	Steps   []Step  `json:"steps,omitempty"` // one per batch; empty in a state saved before parallel batches
}

// StepStatus is where one batch is.
type StepStatus string

const (
	StepPending   StepStatus = ""
	StepRunning   StepStatus = "running"
	StepVerifying StepStatus = "verifying"
	StepGreen     StepStatus = "green"
	StepFailed    StepStatus = "failed"
	StepAccepted  StepStatus = "accepted"
)

// Step is one batch in the schedule: it starts when every batch in After is green or accepted.
type Step struct {
	N        int        `json:"n"`
	Repo     string     `json:"repo,omitempty"`
	After    []int      `json:"after,omitempty"`
	Status   StepStatus `json:"status,omitempty"`
	Attempt  int        `json:"attempt,omitempty"`  // retries used
	Launched bool       `json:"launched,omitempty"` // the main thread launched its implementer
}

// Running lists the batches that are running (not yet returned), in order.
func (s State) Running() []int {
	var out []int
	for _, st := range s.Steps {
		if st.Status == StepRunning {
			out = append(out, st.N)
		}
	}
	return out
}

// Unlaunched lists the running batches whose implementer was not launched yet, in order.
func (s State) Unlaunched() []int {
	var out []int
	for _, st := range s.Steps {
		if st.Status == StepRunning && !st.Launched {
			out = append(out, st.N)
		}
	}
	return out
}
