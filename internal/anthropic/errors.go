package anthropic

import (
	"encoding/json"
	"net/http"
)

// Error type discriminators of the API error envelope.
const (
	ErrInvalidRequest  = "invalid_request_error"
	ErrAuthentication  = "authentication_error"
	ErrPermission      = "permission_error"
	ErrNotFound        = "not_found_error"
	ErrRequestTooLarge = "request_too_large"
	ErrRateLimit       = "rate_limit_error"
	ErrAPI             = "api_error"
	ErrOverloaded      = "overloaded_error"
)

// ErrorResponse is the API error envelope.
type ErrorResponse struct {
	Type  string      `json:"type"`
	Error ErrorDetail `json:"error"`
}

// ErrorDetail carries the machine-readable type and the human-readable message.
type ErrorDetail struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// NewError builds an error envelope.
func NewError(errType, message string) ErrorResponse {
	return ErrorResponse{
		Type:  "error",
		Error: ErrorDetail{Type: errType, Message: message},
	}
}

// ErrorTypeForStatus maps an HTTP status code to the error type the API uses
// for it.
func ErrorTypeForStatus(status int) string {
	switch status {
	case http.StatusBadRequest:
		return ErrInvalidRequest
	case http.StatusUnauthorized:
		return ErrAuthentication
	case http.StatusForbidden:
		return ErrPermission
	case http.StatusNotFound:
		return ErrNotFound
	case http.StatusRequestEntityTooLarge:
		return ErrRequestTooLarge
	case http.StatusTooManyRequests:
		return ErrRateLimit
	case 529:
		return ErrOverloaded
	default:
		return ErrAPI
	}
}

// WriteError sends an error envelope with the given status code.
func WriteError(w http.ResponseWriter, status int, errType, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(NewError(errType, message))
}
