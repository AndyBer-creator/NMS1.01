package http

import (
	_ "embed"
	"net/http"
)

//go:embed spec/openapi.yaml
var embeddedOpenAPISpec []byte

//go:embed spec/security.txt
var embeddedSecurityTxt []byte

func serveOpenAPISpec(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	_, _ = w.Write(embeddedOpenAPISpec)
}

func serveWellKnownSecurityTxt(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write(embeddedSecurityTxt)
}
