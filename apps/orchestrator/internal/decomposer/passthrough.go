package decomposer

// PassthroughPlan returns a single-node plan that just answers the prompt.
// Used when retries are exhausted and we still need to give the user something.
func PassthroughPlan(prompt string) *Plan {
	return &Plan{
		Version: "plan.v1",
		Nodes: []Node{{
			ID:        "n1",
			Role:      RoleSynthesizer,
			Payload:   prompt,
			DependsOn: []string{},
			NeedsRAG:  false,
			Requires: Requirements{
				TaskClass:       ClassReasoning,
				Modality:        ModalityText,
				LatencyBudgetMS: 20000,
				MaxCostUSD:      0.20,
			},
		}},
		Edges: []Edge{},
	}
}
