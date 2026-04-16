package http

import (
	"encoding/json"
	"net/http"
)

const maxJSONBodyBytes int64 = 1 << 20 // 1 MiB

func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	return json.NewDecoder(r.Body).Decode(dst)
}
