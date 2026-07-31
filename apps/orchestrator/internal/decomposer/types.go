// Package decomposer turns a user prompt into a Plan (a DAG of sub-tasks).
//
// The decomposer reuses the providers.Streamer interface from Phase 1 but
// treats the call as non-streaming: it accumulates all tokens into a single
// string, parses it as JSON, and validates the result against the embedded
// plan.schema.json.
//
// On invalid output, the decomposer retries up to `Deps.Retries` times with
// the validation error appended to the system prompt. If all attempts fail,
// it returns PassthroughPlan(prompt) — a single-node synthesizer plan that
// just answers the original prompt.
//
// Implementation outline:
//
//  1. Read packages/prompts/decomposer.v1.md (embedded as a Go string).
//  2. Send the prompt to an LLM with response_format = plan.schema.json
//     (for OpenAI; Anthropic uses tool-use).
//  3. Validate the returned JSON against the schema.
//  4. Return a *plan.Plan or the passthrough fallback.
package decomposer

// Role is the canonical sub-task role. The decomposer must use one of these.
type Role string

const (
	RoleResearcher  Role = "researcher"
	RolePlanner     Role = "planner"
	RoleCoder       Role = "coder"
	RoleSummarizer  Role = "summarizer"
	RoleCritic      Role = "critic"
	RoleSynthesizer Role = "synthesizer"
	RoleFactchecker Role = "factchecker"
	RoleTranslator  Role = "translator"
)

// TaskClass describes what kind of work the sub-task does. Phase 3 routes on it.
type TaskClass string

const (
	ClassReasoning     TaskClass = "reasoning"
	ClassSummarization TaskClass = "summarization"
	ClassCreative      TaskClass = "creative"
	ClassFactual       TaskClass = "factual"
	ClassCode          TaskClass = "code"
	ClassTranslation   TaskClass = "translation"
	ClassVision        TaskClass = "vision"
	ClassRouting       TaskClass = "routing"
)

// Modality is the input/output modality of the sub-task.
type Modality string

const (
	ModalityText  Modality = "text"
	ModalityImage Modality = "image"
	ModalityAudio Modality = "audio"
	ModalityCode  Modality = "code"
)

// Requirements describes what the node needs from the router (Phase 3).
type Requirements struct {
	TaskClass       TaskClass `json:"task_class"`
	Modality        Modality  `json:"modality"`
	LatencyBudgetMS int       `json:"latency_budget_ms"`
	MaxCostUSD      float64   `json:"max_cost_usd"`
}

// Node is one sub-task in the plan.
type Node struct {
	ID        string       `json:"id"`
	Role      Role         `json:"role"`
	Payload   string       `json:"payload"`
	DependsOn []string     `json:"depends_on"`
	NeedsRAG  bool         `json:"needs_rag"`
	Requires  Requirements `json:"requires"`
}

// Edge connects two nodes: an edge from A to B means B depends on A.
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Plan is the JSON object the decomposer produces. `Version` is fixed to
// "plan.v1" so consumers can version-detect.
type Plan struct {
	Version string `json:"version"`
	Nodes   []Node `json:"nodes"`
	Edges   []Edge `json:"edges"`
}