package design

import "fmt"

// Labels of the options of the questions nxy asks; the app matches on them.
const (
	LabelSave          = "OK, guardar"
	LabelKeepDebating  = "Seguir debatiendo"
	LabelClose         = "Cerrar y planificar"
	LabelKeepDesigning = "Seguir diseñando"
	LabelToEngineer    = "Pasar a engineer"
	LabelAnswerHere    = "Responder acá"
)

// ProposeQuestion is the AskUserQuestion text that saves decision D<n>.
func ProposeQuestion(id string, n int) string {
	return fmt.Sprintf("nxy feature %s: ¿guardar la decisión D%d?", id, n)
}

// CloseQuestion is the AskUserQuestion text that closes the design.
func CloseQuestion(id string) string {
	return fmt.Sprintf("nxy feature %s: ¿cerrar el diseño y planificar?", id)
}

// ForkQuestion is the AskUserQuestion text that picks one option of the open fork. Its options are
// the labels saved by design fork, verbatim.
func ForkQuestion(id, fork string) string {
	return fmt.Sprintf("nxy feature %s: ¿qué opción elegís para %s?", id, fork)
}

// OfferQuestion is the AskUserQuestion text of the offer to move to engineer.
func OfferQuestion(id string) string {
	return fmt.Sprintf("nxy feature %s: ¿pasar a engineer para decidir esto con vos?", id)
}
