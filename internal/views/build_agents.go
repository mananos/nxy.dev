package views

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mananos/nxy.dev/v2/internal/domain"
	"github.com/mananos/nxy.dev/v2/internal/metrics"
)

// MaxAttempts is the most attempts a batch gets (the first and one retry).
const MaxAttempts = 2

// LiveAgent is a subagent with no agent_runs row yet (it is running, or its stop was not seen).
type LiveAgent struct {
	AgentID   string
	Role      string
	Model     string
	Effort    string
	Batch     int
	Usage     metrics.Finished
	StartedAt int64
	Tool      *ToolUse
	Tools     []ToolCount
}

// MainThread is the main conversation of the feature.
type MainThread struct {
	Model  string
	Effort string
	Usage  metrics.Finished
	Tools  []ToolCount
	Tool   *ToolUse // the last tool it used, when known
}

// AgentsIn is what the agent tree needs: the feature, the finished runs, the running ones and
// what each agent received (already resolved, with its own Cited) and learned, by agent id.
type AgentsIn struct {
	FeatureIn
	Runs     []domain.AgentRun
	Live     []LiveAgent
	Main     *MainThread
	Received map[string][]MemRef
	Learned  map[string][]string
}

// agentData is one agent, from a run or a live one.
type agentData struct {
	id, role, model, effort string
	batch                   int
	in, out, cr, cw         int64
	cost                    float64
	est                     bool
	start, end              int64
	live                    bool
	tool                    *ToolUse
	tools                   []ToolCount
}

// BuildAgents builds the agent tree: the main thread, then planner (scouts under it), one node per
// batch (fixers under it), reviewer, reviewer-quick, refuter and documenter.
func BuildAgents(in AgentsIn) Agents {
	f := in.Feature
	var data []agentData
	for _, r := range in.Runs {
		data = append(data, agentData{
			id: r.AgentID, role: r.Role, model: r.Model, effort: r.Effort, batch: r.Batch,
			in: r.TokensIn, out: r.TokensOut, cr: r.CacheRead, cw: r.CacheWrite,
			cost: r.CostUSD, est: r.CostEstimated, start: r.StartedAt, end: r.EndedAt,
		})
	}
	for _, l := range in.Live {
		u := l.Usage.Usage
		data = append(data, agentData{
			id: l.AgentID, role: l.Role, model: l.Model, effort: l.Effort, batch: l.Batch,
			in: int64(u.Input), out: int64(u.Output),
			cr: int64(u.CacheRead), cw: int64(u.CacheWrite5m + u.CacheWrite1h),
			cost: l.Usage.USD, est: l.Usage.USDEstimated || l.Usage.USDPartial, start: l.StartedAt,
			live: true, tool: l.Tool, tools: l.Tools,
		})
	}
	sort.SliceStable(data, func(i, j int) bool { return data[i].start < data[j].start })

	b := &agentsBuilder{in: in, now: in.Now}
	root := AgentNode{
		ID: "main", Role: "main", Label: "Hilo principal", Batch: 0,
		ElapsedMs: elapsedOf(in.FeatureIn),
		After:     []int{}, WaitingOn: []int{}, Tools: []ToolCount{},
		Received: []MemRef{}, Learned: []string{}, Children: []AgentNode{},
	}
	if m := in.Main; m != nil {
		root.Model, root.Effort = m.Model, m.Effort
		b.numbers(&root, usageOf(m.Usage), m.Tools)
		root.CurrentTool = m.Tool
	}
	b.total += root.CostUSD
	b.est = b.est || root.CostEstimated

	used := map[int]bool{}
	take := func(role string) []int {
		var idx []int
		for i, d := range data {
			if d.role == role {
				idx = append(idx, i)
			}
		}
		return idx
	}

	// planner and its scouts
	planners := take("planner")
	scouts := take("scout")
	for k, i := range planners {
		used[i] = true
		n := b.node(data[i], "Planner")
		if k == 0 {
			for _, s := range scouts {
				used[s] = true
				n.Children = append(n.Children, b.node(data[s], "Scout"))
			}
		}
		root.Children = append(root.Children, n)
	}
	if len(planners) == 0 && len(scouts) > 0 {
		for _, s := range scouts {
			used[s] = true
			root.Children = append(root.Children, b.node(data[s], "Scout"))
		}
	}

	// one node per batch
	steps := f.State.Steps
	status := map[int]domain.StepStatus{}
	for _, st := range steps {
		status[st.N] = st.Status
	}
	for _, st := range steps {
		root.Children = append(root.Children, b.stepNode(st, data, used, status))
	}

	// reviewers and documenter
	for _, role := range []string{"reviewer", "reviewer-quick", "refuter", "documenter"} {
		for _, i := range take(role) {
			used[i] = true
			root.Children = append(root.Children, b.node(data[i], roleLabel(role)))
		}
	}

	// runs that got no node (earlier attempts of a pending batch, a batch with no step): show them
	// so the header cost matches the sum of every run
	for i, d := range data {
		if !used[i] {
			used[i] = true
			root.Children = append(root.Children, b.node(d, roleLabel(d.role)))
		}
	}

	h := Header(in.FeatureIn)
	h.CostUSD = b.total
	h.CostEstimated = b.est
	return Agents{Feature: h, Root: root}
}

type agentsBuilder struct {
	in    AgentsIn
	now   time.Time
	total float64
	est   bool
}

func usageOf(f metrics.Finished) agentData {
	u := f.Usage
	return agentData{
		in: int64(u.Input), out: int64(u.Output),
		cr: int64(u.CacheRead), cw: int64(u.CacheWrite5m + u.CacheWrite1h),
		cost: f.USD, est: f.USDEstimated || f.USDPartial,
	}
}

func (b *agentsBuilder) numbers(n *AgentNode, d agentData, tools []ToolCount) {
	n.TokensIn, n.TokensOut, n.CacheRead, n.CacheWrite = d.in, d.out, d.cr, d.cw
	n.CostUSD, n.CostEstimated = d.cost, d.est
	if tot := d.in + d.cr + d.cw; tot > 0 {
		p := float64(d.cr) / float64(tot) * 100
		n.CacheHitPct = &p
	}
	if len(tools) > 0 {
		n.Tools = append([]ToolCount{}, tools...)
	}
}

// node is a plain agent node (no batch schedule).
func (b *agentsBuilder) node(d agentData, label string) AgentNode {
	n := AgentNode{
		ID: d.id, Role: d.role, Label: label, Batch: d.batch,
		Status: StepGreen, Model: d.model, Effort: d.effort,
		After: []int{}, WaitingOn: []int{}, Tools: []ToolCount{},
		Received: []MemRef{}, Learned: []string{}, Children: []AgentNode{},
		ElapsedMs: UnknownMs,
	}
	b.fill(&n, d)
	return n
}

// fill puts the numbers, time, tool, received and learned of d on n and adds its cost to the total.
func (b *agentsBuilder) fill(n *AgentNode, d agentData) {
	b.numbers(n, d, d.tools)
	if d.live {
		n.Status = StepRunning
		n.CurrentTool = d.tool
		if d.start > 0 {
			n.ElapsedMs = max(b.now.UnixMilli()-d.start, 0)
		}
	} else if d.start > 0 && d.end >= d.start {
		n.ElapsedMs = d.end - d.start
	}
	n.Received = append(n.Received, b.in.Received[d.id]...)
	for _, l := range b.in.Learned[d.id] {
		n.Learned = append(n.Learned, strings.TrimSpace(strings.TrimPrefix(l, "learned:")))
	}
	b.total += d.cost
	b.est = b.est || d.est
}

func (b *agentsBuilder) stepNode(st domain.Step, data []agentData, used map[int]bool, status map[int]domain.StepStatus) AgentNode {
	f := b.in.Feature
	n := AgentNode{
		ID: fmt.Sprintf("batch-%d", st.N), Role: "implementer", Label: fmt.Sprintf("Lote %d", st.N),
		Batch: st.N, Repo: st.Repo, Status: stepStatus(st.Status),
		After: []int{}, WaitingOn: []int{}, Tools: []ToolCount{},
		Received: []MemRef{}, Learned: []string{}, Children: []AgentNode{},
		Attempt: st.Attempt + 1, Attempts: MaxAttempts, ElapsedMs: StepElapsed(st, b.now),
	}
	if f.Plan != nil {
		for _, pb := range f.Plan.Batches {
			if pb.N == st.N {
				n.Title = pb.Title
				if n.Repo == "" {
					n.Repo = pb.Repo
				}
			}
		}
	}
	for _, a := range st.After {
		n.After = append(n.After, a)
		if s := status[a]; s != domain.StepGreen && s != domain.StepAccepted {
			n.WaitingOn = append(n.WaitingOn, a)
		}
	}
	if st.Status == domain.StepPending {
		n.Attempt = 0 // not started: no attempt yet
		return n
	}

	// the agent of the step: by lane, else the last implementer of the batch
	lane, _ := f.LaneOf(st.N)
	pick := -1
	for i, d := range data {
		if used[i] || d.role == "fixer" {
			continue
		}
		if lane.AgentID != "" && d.id == lane.AgentID {
			pick = i
			break
		}
		if d.batch == st.N && d.role == "implementer" {
			pick = i
		}
	}
	if pick >= 0 {
		used[pick] = true
		d := data[pick]
		n.ID, n.Model, n.Effort = d.id, d.model, d.effort
		keep := n.ElapsedMs
		b.fill(&n, d)
		n.ElapsedMs = keep
		n.Status = stepStatus(st.Status)
	}
	// earlier attempts of the batch (implementers that are not the picked one), then fixers
	for _, role := range []string{"implementer", "fixer"} {
		for i, d := range data {
			if used[i] || d.role != role || d.batch != st.N {
				continue
			}
			used[i] = true
			n.Children = append(n.Children, b.node(d, roleLabel(role)))
		}
	}
	if st.Status == domain.StepFailed {
		fl := &Failure{Lines: lastLines(f.ProofOf(st.N).Failure, 6)}
		if f.Plan != nil {
			for _, pb := range f.Plan.Batches {
				if pb.N == st.N {
					fl.Test = pb.Test.Accept
					if fl.Test == "" {
						fl.Test = pb.Test.Name
					}
				}
			}
		}
		n.Failure = fl
	}
	return n
}

func stepStatus(s domain.StepStatus) StepStatus {
	switch s {
	case domain.StepRunning:
		return StepRunning
	case domain.StepVerifying:
		return StepVerifying
	case domain.StepGreen:
		return StepGreen
	case domain.StepFailed:
		return StepFailed
	case domain.StepAccepted:
		return StepAccepted
	}
	return StepPending
}

func roleLabel(role string) string {
	switch role {
	case "reviewer":
		return "Revisor"
	case "reviewer-quick":
		return "Revisor rápido"
	case "refuter":
		return "Refutador"
	case "documenter":
		return "Documentador"
	case "implementer":
		return "Intento anterior"
	case "fixer":
		return "Fixer"
	}
	return role
}

// lastLines is the last n non-empty lines of s, trimmed on the right.
func lastLines(s string, n int) []string {
	var all []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimRight(l, " \t\r"); strings.TrimSpace(l) != "" {
			all = append(all, l)
		}
	}
	if len(all) > n {
		all = all[len(all)-n:]
	}
	if all == nil {
		all = []string{}
	}
	return all
}
