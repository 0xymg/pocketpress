// Package middleware provides small, dependency-free net/http middleware
// for PocketPress: request logging and panic recovery. Both are chained
// around the application handler in internal/http.
package middleware

import (
	"encoding/json"
	"net/http"
)

// errorEnvelope is PocketPress's standard JSON error response shape:
//
//	{"error":{"code":"...","message":"...","details":null}}
type errorEnvelope struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details"`
}

// WriteJSON writes v as a JSON response body with the given status code.
// It is exported so callers outside this package (the router's own
// handlers) can produce responses using the same envelope helpers as the
// middleware, without duplicating the encoding boilerplate.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteError writes PocketPress's standard error envelope with a nil
// details field.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteErrorDetails(w, status, code, message, nil)
}

// WriteErrorDetails writes PocketPress's standard error envelope with a
// populated details field. Validation handlers use it to attach a
// field-name to human-message map (code "validation_failed").
func WriteErrorDetails(w http.ResponseWriter, status int, code, message string, details any) {
	WriteJSON(w, status, errorEnvelope{Error: errorBody{Code: code, Message: message, Details: details}})
}
