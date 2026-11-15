package api

import (
	"encoding/json"
	"net/http"
)

// writeJSON sends v as a JSON response with the given status code.
//
// The header must be set before WriteHeader: once the status line is sent,
// headers can't change. v is `any` (an alias for interface{}), so it takes
// a struct, a map or a slice.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// The status is already sent, so an encoding error can't be reported to
	// the client any more. Ignoring it is the usual choice here.
	_ = json.NewEncoder(w).Encode(v)
}

// errorResponse is the body of every error: {"error": "..."}.
type errorResponse struct {
	Error string `json:"error"`
}
