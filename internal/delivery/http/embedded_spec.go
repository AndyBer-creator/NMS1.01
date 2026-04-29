package http

import (
	_ "embed"
	"encoding/json"
	"net/http"

	yaml "gopkg.in/yaml.v2"
)

//go:embed spec/openapi.yaml
var embeddedOpenAPISpec []byte
var embeddedOpenAPISpecJSON []byte

//go:embed spec/security.txt
var embeddedSecurityTxt []byte

func serveOpenAPISpec(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	_, _ = w.Write(embeddedOpenAPISpec)
}

func serveOpenAPISpecJSON(w http.ResponseWriter, r *http.Request) {
	if len(embeddedOpenAPISpecJSON) == 0 {
		http.Error(w, "openapi json unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(embeddedOpenAPISpecJSON)
}

func init() {
	var parsed any
	if err := yaml.Unmarshal(embeddedOpenAPISpec, &parsed); err != nil {
		return
	}
	normalized := normalizeYAMLForJSON(parsed)
	b, err := json.Marshal(normalized)
	if err != nil {
		return
	}
	embeddedOpenAPISpecJSON = b
}

func normalizeYAMLForJSON(v any) any {
	switch x := v.(type) {
	case map[any]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[toString(k)] = normalizeYAMLForJSON(val)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[k] = normalizeYAMLForJSON(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i := range x {
			out[i] = normalizeYAMLForJSON(x[i])
		}
		return out
	default:
		return x
	}
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func serveWellKnownSecurityTxt(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write(embeddedSecurityTxt)
}
