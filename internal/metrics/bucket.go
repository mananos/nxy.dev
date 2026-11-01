package metrics

import (
	"bytes"
	"encoding/json"
	"math"

	"github.com/mananos/nxy.dev/internal/pricing"
)

// Call is one API call as a host adapter hands it to the core (JSON keys of the 1.x).
type Call struct {
	Key       string        `json:"key"`
	Ts        int64         `json:"ts"`
	Model     string        `json:"model"`
	Usage     pricing.Usage `json:"usage"`
	Effort    *string       `json:"effort"`
	Skill     *string       `json:"skill"`
	PromptKey *string       `json:"promptKey"`
	AgentID   *string       `json:"agentId"`
	// GapMs is the time since the previous call of the same thread; nil for the first.
	GapMs *int64 `json:"gapMs"`
	// After is what the thread waited on before this call: "subagent", "compact" or nil.
	After *string `json:"after"`
}

// ModelBasis is a model -> price-basis map that keeps insertion order (JS Map / Object.fromEntries).
type ModelBasis struct {
	keys   []string
	values map[string]string
}

// Set stores basis for model; an existing model keeps its position.
func (m *ModelBasis) Set(model, basis string) {
	if m.values == nil {
		m.values = map[string]string{}
	}
	if _, ok := m.values[model]; !ok {
		m.keys = append(m.keys, model)
	}
	m.values[model] = basis
}

// Get returns the basis of model.
func (m ModelBasis) Get(model string) (string, bool) {
	v, ok := m.values[model]
	return v, ok
}

// Keys returns the models in insertion order.
func (m ModelBasis) Keys() []string { return append([]string{}, m.keys...) }

// MarshalJSON writes an object in insertion order.
func (m ModelBasis) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range m.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		kj, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		vj, err := json.Marshal(m.values[k])
		if err != nil {
			return nil, err
		}
		buf.Write(kj)
		buf.WriteByte(':')
		buf.Write(vj)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// Bucket accumulates a group of calls. USD is the sum of what has a price; USDUnknownCalls counts
// the calls left out (model without price) and UnknownModels says which.
type Bucket struct {
	Calls             int
	Usage             pricing.Usage
	USD               float64
	USDUnknownCalls   int
	UnknownModels     []string
	USDEstimatedCalls int
	EstimatedModels   ModelBasis
	ContextPeak       float64

	unknownSeen map[string]bool
}

// NewBucket returns an empty bucket.
func NewBucket() *Bucket {
	return &Bucket{Usage: pricing.EmptyUsage(), unknownSeen: map[string]bool{}}
}

// Add adds one call (mutates). Cost is recomputed from the price table each time.
func (b *Bucket) Add(c Call) *Bucket {
	b.Calls++
	pricing.AddUsage(&b.Usage, c.Usage)
	usd, ok := pricing.CostFor(c.Model, c.Usage, nil)
	if !ok {
		b.USDUnknownCalls++
		if !b.unknownSeen[c.Model] {
			b.unknownSeen[c.Model] = true
			b.UnknownModels = append(b.UnknownModels, c.Model)
		}
	} else {
		b.USD += usd
		if basis, est := pricing.EstimateBasis(c.Model); est && usd > 0 {
			b.USDEstimatedCalls++
			b.EstimatedModels.Set(c.Model, basis)
		}
	}
	b.ContextPeak = math.Max(b.ContextPeak, pricing.TotalInput(c.Usage))
	return b
}

// Finished is a closed bucket for output: USD is the known part (never null) and UsdPartial warns
// that some calls could not be priced.
type Finished struct {
	Calls             int           `json:"calls"`
	Usage             pricing.Usage `json:"usage"`
	USD               float64       `json:"usd"`
	USDUnknownCalls   int           `json:"usdUnknownCalls"`
	UnknownModels     []string      `json:"unknownModels"`
	USDEstimatedCalls int           `json:"usdEstimatedCalls"`
	EstimatedModels   ModelBasis    `json:"estimatedModels"`
	ContextPeak       float64       `json:"contextPeak"`
	USDPartial        bool          `json:"usdPartial"`
	USDEstimated      bool          `json:"usdEstimated"`
	CacheHitPct       *float64      `json:"cacheHitPct"`
	TotalInput        float64       `json:"totalInput"`
}

// Finish closes the bucket (finishBucket of the 1.x).
func (b *Bucket) Finish() Finished {
	return Finished{
		Calls:             b.Calls,
		Usage:             b.Usage,
		USD:               b.USD,
		USDUnknownCalls:   b.USDUnknownCalls,
		UnknownModels:     append([]string{}, b.UnknownModels...),
		USDEstimatedCalls: b.USDEstimatedCalls,
		EstimatedModels:   b.EstimatedModels,
		ContextPeak:       b.ContextPeak,
		USDPartial:        b.USDUnknownCalls > 0,
		USDEstimated:      b.USDEstimatedCalls > 0,
		CacheHitPct:       pricing.CacheHitPct(b.Usage),
		TotalInput:        pricing.TotalInput(b.Usage),
	}
}
