package filter

import (
	"testing"

	"github.com/mananos/nxy.dev/v2/internal/goldentest"
)

func TestDescribeEngineGolden(t *testing.T) {
	for _, c := range goldentest.Load(t, "filterstatus") {
		t.Run(c.File+"/"+c.Name, func(t *testing.T) {
			var in struct {
				Engine          string  `json:"engine"`
				Reason          string  `json:"reason"`
				RtkPath         *string `json:"rtkPath"`
				RtkVersion      *string `json:"rtkVersion"`
				RtkHookDetected bool    `json:"rtkHookDetected"`
				RgAvailable     bool    `json:"rgAvailable"`
				RgInstalledAt   *string `json:"rgInstalledAt"`
			}
			var engine string
			c.In(t, "info", &in)
			c.In(t, "engine", &engine)
			info := EngineInfo{Engine: in.Engine, Reason: in.Reason, RtkHookDetected: in.RtkHookDetected}
			if in.RtkPath != nil {
				info.RtkPath = *in.RtkPath
			}
			if in.RtkVersion != nil {
				info.RtkVersion = *in.RtkVersion
			}
			rg := RgState{Available: in.RgAvailable}
			if in.RgInstalledAt != nil {
				rg.InstalledAt = *in.RgInstalledAt
			}
			goldentest.Check(t, c, map[string]any{"text": DescribeEngine(info, rg, Settings{Engine: engine})})
		})
	}
}
