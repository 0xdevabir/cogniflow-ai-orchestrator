// Command bandit-learn reads the orchestrator's JSONL feedback log and
// prints a per-task-class winner + bench-weight boost recommendation.
//
// Usage:
//
//	bandit-learn -log ./data/bandit.jsonl
//	bandit-learn -log ./data/bandit.jsonl -min 50 -json -out rec.json
//
// Exit codes:
//
//	0 success
//	1 bad args
//	2 read failure
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/cogniflow/orchestrator/internal/banditlearn"
)

func main() {
	logPath := flag.String("log", "./data/bandit.jsonl", "path to bandit feedback JSONL")
	minCount := flag.Int("min", 5, "minimum samples per (task_class, model) bucket")
	asJSON := flag.Bool("json", false, "emit JSON instead of human-readable summary")
	outPath := flag.String("out", "", "write output to this file (default: stdout)")
	flag.Parse()

	rec, err := banditlearn.LearnFile(*logPath, *minCount)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bandit-learn: %v\n", err)
		os.Exit(2)
	}

	var body []byte
	if *asJSON {
		body, err = rec.JSON()
		if err != nil {
			fmt.Fprintf(os.Stderr, "bandit-learn: json marshal: %v\n", err)
			os.Exit(2)
		}
	} else {
		body = []byte(rec.String())
	}

	if *outPath != "" {
		if err := os.WriteFile(*outPath, body, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "bandit-learn: write: %v\n", err)
			os.Exit(2)
		}
	} else {
		_, _ = os.Stdout.Write(body)
	}
}
