package domain

// DocLine is one line of a doc that names what changed.
type DocLine struct {
	Line int    `json:"line"`
	Text string `json:"text"`
}

// DocFile is a doc and the lines where it names the change.
type DocFile struct {
	Path  string    `json:"path"`
	Lines []DocLine `json:"lines"`
}

// Docs is what nxy found about the docs a change concerns.
type Docs struct {
	// Terms are the words searched: one per changed code file.
	Terms []string  `json:"terms,omitempty"`
	Docs  []DocFile `json:"docs,omitempty"`
	// WikiDir is the folder the repo's CI publishes to a wiki, "" when none.
	WikiDir string `json:"wiki_dir,omitempty"`
}

// Paths lists the paths of the docs.
func (d Docs) Paths() []string {
	out := make([]string, len(d.Docs))
	for i, f := range d.Docs {
		out[i] = f.Path
	}
	return out
}
