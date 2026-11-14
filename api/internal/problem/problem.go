// Package problem writes RFC 9457 problem details responses.
package problem

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type body struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// Write sends a problem details response. detail is shown to the client, so it must not leak internals.
func Write(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body{
		Type:   "about:blank",
		Title:  http.StatusText(status),
		Status: status,
		Detail: detail,
	})
}

// Error is returned by a handler to answer with a problem response. Detail is shown to the client.
type Error struct {
	Status int
	Detail string
}

func New(status int, detail string) *Error { return &Error{Status: status, Detail: detail} }

func (e *Error) Error() string {
	return fmt.Sprintf("%d %s: %s", e.Status, http.StatusText(e.Status), e.Detail)
}
