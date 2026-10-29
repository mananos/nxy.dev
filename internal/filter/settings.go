package filter

// Settings is the filter configuration the use case needs (modules.filter and filter.*).
type Settings struct {
	Enabled         bool
	Engine          string
	ExcludeCommands []string
	OnlyCommands    []string
	AutoAllow       bool
}

// EngineInfo is the resolved filter engine (which tool rewrites commands, and where it lives).
type EngineInfo struct {
	Engine          string
	RtkPath         string
	RtkVersion      string
	Reason          string
	RtkHookDetected bool
	ResolvedAt      int64
}
