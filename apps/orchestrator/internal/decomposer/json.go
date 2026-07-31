package decomposer

import "encoding/json"

// Small json helpers — kept in a separate file so the rest of the package
// can use them without importing encoding/json everywhere. Tests use them
// too.

func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }
func jsonUnmarshal(b []byte, v any) error {
	return json.Unmarshal(b, v)
}
