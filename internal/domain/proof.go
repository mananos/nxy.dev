package domain

// Run is one execution of a batch's Accept command, run by the binary.
type Run struct {
	Exit int    `json:"exit"`
	Tail string `json:"tail,omitempty"`
	// Edits is the Proof.Edits counter when the run started.
	Edits int `json:"edits,omitempty"`
}

// Proof is the evidence of one batch: what the binary saw the implementer prove.
type Proof struct {
	Batch int  `json:"batch"`
	Red   *Run `json:"red,omitempty"`
	Green *Run `json:"green,omitempty"`
	// Edits counts the subagent writes in the repo during the batch.
	Edits int `json:"edits,omitempty"`
	// Failure is why the last verdict was red, with the tail, for the retry prompt.
	Failure string `json:"failure,omitempty"`
}
