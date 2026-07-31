# Router Meta Prompt — v1 (Phase 8)

When the bandit log has enough data, a second LLM rewrites the weighted score coefficients
based on observed (task_class, model, was_chosen, user_satisfaction) tuples. The output is a
new coefficient set:

```json
{
  "weights": {
    "reasoning":      { "bench": 0.5, "cost": 0.25, "latency": 0.25 },
    "summarization":  { "bench": 0.3, "cost": 0.5,  "latency": 0.2  },
    "creative":       { "bench": 0.6, "cost": 0.2,  "latency": 0.2  },
    "factual":        { "bench": 0.5, "cost": 0.3,  "latency": 0.2  },
    "code":           { "bench": 0.6, "cost": 0.2,  "latency": 0.2  },
    "translation":    { "bench": 0.2, "cost": 0.5,  "latency": 0.3  }
  }
}
```

Rules:

1. Weights per task_class sum to 1.0.
2. Never set a weight below 0.10.
3. Increase `cost` weight for cheap-fast task classes; `bench` for reasoning/code.
