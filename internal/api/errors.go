package api

import (
	"encoding/json"
	"net/http"
)

// Standard error codes per PRD Section 8b.
const (
	CodeValidationError      = "VALIDATION_ERROR"
	CodeNotFound             = "NOT_FOUND"
	CodeDuplicate            = "DUPLICATE"
	CodeUnauthorized         = "UNAUTHORIZED"
	CodeRateLimited          = "RATE_LIMITED"
	CodeInternalError        = "INTERNAL_ERROR"
	CodeReconciliationLocked = "RECONCILIATION_LOCKED"
	CodeNotImplemented       = "NOT_IMPLEMENTED"
)

// APIError is the standard error payload returned by all endpoints.
type APIError struct {
	Code    string       `json:"code"`
	Message string       `json:"message"`
	Details []FieldError `json:"details,omitempty"`
}

// FieldError describes a single field-level validation failure.
type FieldError struct {
	Field      string `json:"field"`
	Value      string `json:"value"`
	Constraint string `json:"constraint"`
}

// errorEnvelope wraps an APIError in the PRD-specified envelope.
type errorEnvelope struct {
	Error APIError `json:"error"`
}

// --- Convenience constructors ---

// NewValidationError creates a VALIDATION_ERROR with optional field details.
func NewValidationError(msg string, details []FieldError) APIError {
	return APIError{Code: CodeValidationError, Message: msg, Details: details}
}

// NewNotFoundError creates a NOT_FOUND error.
func NewNotFoundError(msg string) APIError {
	return APIError{Code: CodeNotFound, Message: msg}
}

// NewDuplicateError creates a DUPLICATE error.
func NewDuplicateError(msg string) APIError {
	return APIError{Code: CodeDuplicate, Message: msg}
}

// NewUnauthorizedError creates an UNAUTHORIZED error.
func NewUnauthorizedError(msg string) APIError {
	return APIError{Code: CodeUnauthorized, Message: msg}
}

// NewRateLimitedError creates a RATE_LIMITED error.
func NewRateLimitedError(msg string) APIError {
	return APIError{Code: CodeRateLimited, Message: msg}
}

// NewInternalError creates an INTERNAL_ERROR.
func NewInternalError(msg string) APIError {
	return APIError{Code: CodeInternalError, Message: msg}
}

// NewReconciliationLockedError creates a RECONCILIATION_LOCKED error.
func NewReconciliationLockedError(msg string) APIError {
	return APIError{Code: CodeReconciliationLocked, Message: msg}
}

// --- Response helpers ---

// RespondJSON writes data as JSON with the given HTTP status code.
func RespondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if data != nil {
		_ = json.NewEncoder(w).Encode(data)
	}
}

// RespondError writes an APIError wrapped in the standard {"error": {...}} envelope.
func RespondError(w http.ResponseWriter, status int, apiErr APIError) {
	RespondJSON(w, status, errorEnvelope{Error: apiErr})
}
