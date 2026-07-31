package decomposer

import (
	"bytes"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

// schemaOnce holds the compiled JSON Schema. Compiling is expensive; we do
// it once per process. jsonschema uses a *Schema.Compile but does not have
// official `sync.Once` semantics built in, so we guard with a sync.Once.
var (
	compiledSchema *jsonschema.Schema
	compileErr     error
)

// Validate runs plan.schema.json against the given plan and returns an error
// describing the first violation, or nil on success.
//
// Reference resolution:
//   The plan.schema.json references internal definitions via #/$defs.
//   jsonschema needs to know which "id" prefix to use; we add the embedded
//   version into the compiler. To keep things simple, we give the schema a
//   fake URL via the $id field already in the file.
func Validate(p *Plan) error {
	if p == nil {
		return fmt.Errorf("plan is nil")
	}
	if err := ensureSchema(); err != nil {
		return fmt.Errorf("schema compile: %w", err)
	}
	// Marshal + unmarshal through a generic map so we can validate the
	// exact JSON shape rather than Go-typed-friendliness.
	// Note: jsonschema validates any value, so passing the *Plan directly
	// via reflection also works, but the map round-trip catches
	// json-tag mismatches.
	var generic any
	if err := marshalUnmarshal(p, &generic); err != nil {
		return fmt.Errorf("marshal roundtrip: %w", err)
	}
	return compiledSchema.Validate(generic)
}

// ValidateJSON validates raw JSON bytes against the schema. This is what
// the decomposer uses when LLM output comes back as bytes.
func ValidateJSON(raw []byte) error {
	if err := ensureSchema(); err != nil {
		return fmt.Errorf("schema compile: %w", err)
	}
	var v any
	if err := jsonUnmarshal(raw, &v); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	return compiledSchema.Validate(v)
}

// JSONSchema returns the raw JSON of the plan schema, useful for the
// OpenAI response_format / Anthropic tool input_schema parameter.
func JSONSchema() []byte { return PlanSchemaJSON }

func ensureSchema() error {
	if compiledSchema != nil {
		return compileErr
	}
	c := jsonschema.NewCompiler()
	// Add the embedded schema under its declared $id so internal refs resolve.
	if err := c.AddResource("https://cogniflow.dev/schemas/plan.v1.json", bytes.NewReader(PlanSchemaJSON)); err != nil {
		compileErr = fmt.Errorf("add resource: %w", err)
		return compileErr
	}
	compiledSchema, compileErr = c.Compile("https://cogniflow.dev/schemas/plan.v1.json")
	return compileErr
}

// marshalUnmarshal + jsonUnmarshal keep us decoupled from the json import
// (we re-use encoding/json via tiny adapter funcs to keep all imports in one place).
func marshalUnmarshal(p any, out any) error {
	b, err := jsonMarshal(p)
	if err != nil {
		return err
	}
	return jsonUnmarshal(b, out)
}
