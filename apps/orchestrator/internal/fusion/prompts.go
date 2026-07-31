package fusion

import (
	_ "embed"
)

//go:embed prompts/fusion_synthesizer.v1.md
var fusionSynthesizerPrompt string

//go:embed prompts/judge.v1.md
var judgePrompt string

// FusionSynthesizerPrompt returns the synthesizer prompt template.
func FusionSynthesizerPrompt() string { return fusionSynthesizerPrompt }

// JudgePrompt returns the judge prompt template.
func JudgePrompt() string { return judgePrompt }
