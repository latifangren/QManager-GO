package handlers

import (
	"encoding/json"
	"net/http"
)

// Response standard envelope.
type Response struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
	Message string      `json:"message,omitempty"`
}

// JSON writes a JSON payload with status code.
func JSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

// Success returns a standard success JSON envelope.
func Success(w http.ResponseWriter, data interface{}) {
	JSON(w, http.StatusOK, Response{
		Success: true,
		Data:    data,
	})
}

// Error returns a standard error JSON envelope.
func Error(w http.ResponseWriter, status int, errMsg string) {
	JSON(w, status, Response{
		Success: false,
		Error:   errMsg,
	})
}
