package detail

import (
	"bytes"

	"gopkg.in/yaml.v3"
)

// yamlMarshalJSONShaped converts decoded JSON values (map[string]any,
// []any, string, float64, bool, nil) into YAML text using the pinned
// yaml.v3 module. Keys are emitted in the structural order yaml.v3 uses
// for maps (it sorts map keys), which keeps the output deterministic for
// equal inputs — the determinism the resource view requires.
func yamlMarshalJSONShaped(v any) (string, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		enc.Close()
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	return buf.String(), nil
}
