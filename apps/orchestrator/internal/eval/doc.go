// Package eval runs LLM-as-judge faithfulness scoring on final answers.
//
// MVP (Phase 7): per-response, verifies cited claims against RAG doc snippets
// and produces a faithfulness % and hallucination flags.
package eval
