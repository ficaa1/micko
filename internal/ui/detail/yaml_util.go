package detail

import (
	"bytes"

	"gopkg.in/yaml.v3"
)

// yamlMarshalJSONShaped renders decoded JSON values as YAML. yaml.v3 sorts
// map keys, so equal inputs give equal output.
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
