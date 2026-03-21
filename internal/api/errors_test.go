package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRespondJSON_WritesStatusAndContentType(t *testing.T) {
	w := httptest.NewRecorder()

	data := map[string]string{"key": "value"}
	RespondJSON(w, http.StatusOK, data)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json; charset=utf-8", w.Header().Get("Content-Type"))

	var got map[string]string
	err := json.NewDecoder(w.Body).Decode(&got)
	assert.NoError(t, err)
	assert.Equal(t, "value", got["key"])
}

func TestRespondJSON_NilDataWritesNoBody(t *testing.T) {
	w := httptest.NewRecorder()

	RespondJSON(w, http.StatusNoContent, nil)

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Empty(t, w.Body.String())
}

func TestRespondError_WrapsInEnvelope(t *testing.T) {
	w := httptest.NewRecorder()

	apiErr := NewValidationError("bad input", []FieldError{
		{Field: "currency", Value: "EURO", Constraint: "iso4217"},
	})
	RespondError(w, http.StatusBadRequest, apiErr)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var envelope struct {
		Error struct {
			Code    string       `json:"code"`
			Message string       `json:"message"`
			Details []FieldError `json:"details"`
		} `json:"error"`
	}
	err := json.NewDecoder(w.Body).Decode(&envelope)
	assert.NoError(t, err)
	assert.Equal(t, CodeValidationError, envelope.Error.Code)
	assert.Equal(t, "bad input", envelope.Error.Message)
	assert.Len(t, envelope.Error.Details, 1)
	assert.Equal(t, "currency", envelope.Error.Details[0].Field)
	assert.Equal(t, "EURO", envelope.Error.Details[0].Value)
	assert.Equal(t, "iso4217", envelope.Error.Details[0].Constraint)
}

func TestErrorConstructors_SetCorrectCode(t *testing.T) {
	tests := []struct {
		name     string
		err      APIError
		wantCode string
	}{
		{"ValidationError", NewValidationError("msg", nil), CodeValidationError},
		{"NotFound", NewNotFoundError("msg"), CodeNotFound},
		{"Duplicate", NewDuplicateError("msg"), CodeDuplicate},
		{"Unauthorized", NewUnauthorizedError("msg"), CodeUnauthorized},
		{"RateLimited", NewRateLimitedError("msg"), CodeRateLimited},
		{"InternalError", NewInternalError("msg"), CodeInternalError},
		{"ReconciliationLocked", NewReconciliationLockedError("msg"), CodeReconciliationLocked},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.wantCode, tc.err.Code)
			assert.Equal(t, "msg", tc.err.Message)
		})
	}
}
