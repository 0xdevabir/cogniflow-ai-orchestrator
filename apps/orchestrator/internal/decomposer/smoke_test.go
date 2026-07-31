package decomposer

import (
	"testing"
)

// TestValidateJSON_2NodePlan_OK is a smoke test for the real validator +
// real schema — exercises the embedded plan.schema.json with a small
// but realistic plan.
func TestValidateJSON_2NodePlan_OK(t *testing.T) {
	raw := []byte(`{
		"version":"plan.v1",
		"nodes":[
			{"id":"n1","role":"researcher","payload":"research","depends_on":[],"needs_rag":false,
			 "requires":{"task_class":"factual","modality":"text","latency_budget_ms":10000,"max_cost_usd":0.05}},
			{"id":"n2","role":"synthesizer","payload":"merge","depends_on":["n1"],"needs_rag":false,
			 "requires":{"task_class":"reasoning","modality":"text","latency_budget_ms":20000,"max_cost_usd":0.20}}
		],
		"edges":[{"from":"n1","to":"n2"}]
	}`)
	if err := ValidateJSON(raw); err != nil {
		t.Fatalf("expected valid, got: %v", err)
	}
}

// TestValidateJSON_BadRole_Rejected ensures unknown role is rejected.
func TestValidateJSON_BadRole_Rejected(t *testing.T) {
	bad := []byte(`{"version":"plan.v1","nodes":[{"id":"n1","role":"hacker","payload":"x","depends_on":[],"needs_rag":false,"requires":{"task_class":"reasoning","modality":"text","latency_budget_ms":1000,"max_cost_usd":0.1}}],"edges":[]}`)
	if err := ValidateJSON(bad); err == nil {
		t.Fatal("expected error for bad role")
	}
}