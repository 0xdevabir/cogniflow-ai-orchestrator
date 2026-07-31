package decomposer

import _ "embed"

// decomposerPromptV1 is the versioned prompt for the decomposer.
// Edit packages/prompts/decomposer.v1.md and re-copy, or bump to v2.
//
//go:embed prompts/decomposer.v1.md
var decomposerPromptV1 string

// CurrentPrompt returns the active decomposer prompt. Bumping the version
// is just a matter of replacing the file and changing the variable.
func CurrentPrompt() string { return decomposerPromptV1 }

// PlanSchemaJSON is the embedded JSON Schema for the Plan object.
//
//go:embed schemas/plan.schema.json
var PlanSchemaJSON []byte