package deepseek

import (
	"errors"
	"fmt"
)

// StatusError is a non-200 reply from the DeepSeek web API. Callers that need
// to tell a rate limit from an expired login from a dead chat session inspect
// StatusCode instead of matching error text.
type StatusError struct {
	// Op names the endpoint that answered, e.g. "chat/completion".
	Op string
	// StatusCode is the HTTP status DeepSeek returned.
	StatusCode int
	// Body is the start of the response body, for diagnostics.
	Body string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("deepseek %s: HTTP %d: %s", e.Op, e.StatusCode, e.Body)
}

// StatusCode returns the HTTP status carried by a *StatusError anywhere in
// err's chain, or 0 when there is none.
func StatusCode(err error) int {
	var se *StatusError
	if errors.As(err, &se) {
		return se.StatusCode
	}
	return 0
}

func newStatusError(op string, code int, body []byte, limit int) *StatusError {
	return &StatusError{Op: op, StatusCode: code, Body: string(body[:min(len(body), limit)])}
}
