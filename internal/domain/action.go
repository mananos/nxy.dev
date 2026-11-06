package domain

import "strconv"

// ActionKind is what has to happen next.
type ActionKind string

const (
	DesignDebate        ActionKind = "design"
	DispatchPlanner     ActionKind = "dispatch_planner"
	AskQuestions        ActionKind = "ask_questions"
	AskNoTests          ActionKind = "ask_no_tests"
	AskApproval         ActionKind = "ask_approval"
	AskChangeNote       ActionKind = "ask_change_note"
	AskClash            ActionKind = "ask_clash"
	AskBranch           ActionKind = "ask_branch"
	OpenPlan            ActionKind = "open_plan"
	DispatchImplementer ActionKind = "dispatch_implementer"
	VerifyBatch         ActionKind = "verify_batch"
	RunAffected         ActionKind = "run_affected"
	DispatchAffectedFix ActionKind = "dispatch_affected_fix"
	AskDecision         ActionKind = "ask_decision"
	DispatchReviewer    ActionKind = "dispatch_reviewer"
	DispatchRefuter     ActionKind = "dispatch_refuter"
	AskFindings         ActionKind = "ask_findings"
	DispatchFixer       ActionKind = "dispatch_fixer"
	CheckDocs           ActionKind = "check_docs"
	AskDocs             ActionKind = "ask_docs"
	DispatchDocumenter  ActionKind = "dispatch_documenter"
	AskRules            ActionKind = "ask_rules"
	Close               ActionKind = "close"
	Notify              ActionKind = "notify"
)

// Action is one step the flow asks for.
type Action struct {
	Kind  ActionKind `json:"kind"`
	Batch int        `json:"batch,omitempty"`
	Retry bool       `json:"retry,omitempty"`
}

type actionInfo struct {
	mainThread bool
	label      string
}

var actionTable = map[ActionKind]actionInfo{
	DesignDebate:        {true, "Diseñar"},
	DispatchPlanner:     {true, "Planificar"},
	AskQuestions:        {true, "Responder preguntas"},
	AskNoTests:          {true, "Decidir sin tests"},
	AskApproval:         {true, "Aprobar plan"},
	AskChangeNote:       {true, "Escribir el cambio"},
	AskClash:            {true, "Repo ocupado"},
	AskBranch:           {true, "Elegir rama"},
	OpenPlan:            {false, "Abrir plan"},
	DispatchImplementer: {true, "Lote"},
	VerifyBatch:         {false, "Verificar lote"},
	RunAffected:         {true, "Correr afectados"},
	DispatchAffectedFix: {true, "Arreglar afectados"},
	AskDecision:         {true, "Decidir"},
	DispatchReviewer:    {true, "Revisar"},
	DispatchRefuter:     {true, "Refutar"},
	AskFindings:         {true, "Elegir hallazgos"},
	DispatchFixer:       {true, "Arreglar hallazgos"},
	CheckDocs:           {false, "Buscar docs"},
	AskDocs:             {true, "Actualizar docs"},
	DispatchDocumenter:  {true, "Actualizar docs"},
	AskRules:            {true, "Reglas del repo"},
	Close:               {false, "Cerrar"},
	Notify:              {false, "Avisar"},
}

// MainThread reports whether the main thread performs the action; false means
// the binary does it itself.
func (a Action) MainThread() bool { return actionTable[a.Kind].mainThread }

// Label is a neutral Spanish name for the action, e.g. "Lote 1".
func (a Action) Label() string {
	l := actionTable[a.Kind].label
	if a.Kind == DispatchImplementer && a.Batch > 0 {
		return l + " " + strconv.Itoa(a.Batch)
	}
	return l
}
