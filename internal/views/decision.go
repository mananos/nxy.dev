package views

// Decision is a pending question for the user. There is one per feature.
type Decision struct {
	ID          string   `json:"id"`
	FeatureID   string   `json:"feature_id"`
	Repo        string   `json:"repo"`
	Kind        string   `json:"kind"` // approval|batch_failed|repo_clash|rules|questions|fork
	Header      string   `json:"header"`
	Question    string   `json:"question"`
	Context     []string `json:"context"`
	MultiSelect bool     `json:"multi_select"`
	AskedAt     string   `json:"asked_at"`
	Fork        *ForkRef `json:"fork"` // only with Kind "fork": it is read here and answered in Ingeniero
	Options     []Option `json:"options"`
}

// ForkRef says which design fork a question is: "2 of 4".
type ForkRef struct {
	N     int    `json:"n"`
	Of    int    `json:"of"`
	Title string `json:"title"`
}

type Option struct {
	Label       string `json:"label"`
	Description string `json:"description"`
	Recommended bool   `json:"recommended"`
	Action      Action `json:"action"`
}
