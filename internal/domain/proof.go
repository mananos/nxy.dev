package domain

// Run is one execution of a batch's Accept command, run by the binary.
type Run struct {
	Exit int    `json:"exit"`
	Tail string `json:"tail,omitempty"`
	// Edits is the Proof.Edits counter when the run started.
	Edits int `json:"edits,omitempty"`
	// CodeFirst is the Proof.CodeFirst when this red was recorded.
	CodeFirst []string `json:"code_first,omitempty"`
}

// CommandRun is one command run for the affected tests.
type CommandRun struct {
	Dir      string `json:"dir,omitempty"`
	Command  string `json:"command"`
	Exit     int    `json:"exit"`
	Tail     string `json:"tail,omitempty"`
	TimedOut bool   `json:"timed_out,omitempty"`
	// Row is the runner row the command came from; Repo says it is a row of .nxy/runners.json.
	Row  string `json:"row,omitempty"`
	Repo bool   `json:"repo,omitempty"`
	// RepoName is the repo of the feature the command ran in (only with several repos).
	RepoName string `json:"repo_name,omitempty"`
}

// AffectedRun is the record of the affected tests of a feature.
type AffectedRun struct {
	// Source is where the tests came from: "codegraph", "nombres", "suite" or "".
	Source string `json:"source,omitempty"`
	// Note is the Spanish fallback reason or hint.
	Note string `json:"note,omitempty"`
	// Reason is why the run is unverified (kept apart from Note so it is shown once).
	Reason    string       `json:"reason,omitempty"`
	Tests     []string     `json:"tests,omitempty"`
	Unmatched []string     `json:"unmatched,omitempty"`
	Runs      []CommandRun `json:"runs,omitempty"`
	// Failure is why the run was red, with the tails, for the fix prompt.
	Failure string `json:"failure,omitempty"`
}

// Proof is the evidence of one batch: what the binary saw the implementer prove.
type Proof struct {
	Batch int  `json:"batch"`
	Red   *Run `json:"red,omitempty"`
	Green *Run `json:"green,omitempty"`
	// Edits counts the subagent writes in the repo during the batch.
	Edits int `json:"edits,omitempty"`
	// CodeFirst lists the non-test files written in the batch before the red (at most 5).
	CodeFirst []string `json:"code_first,omitempty"`
	// Failure is why the last verdict was red, with the tail, for the retry prompt.
	Failure string `json:"failure,omitempty"`
}
