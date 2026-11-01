package deepseek

import (
	"testing"
)

func TestCleanToken(t *testing.T) {
	jwtSample := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.doNotRevealSecretSignature12345"

	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    jwtSample,
			expected: jwtSample,
		},
		{
			input:    "Bearer " + jwtSample,
			expected: jwtSample,
		},
		{
			input:    `{"value":"` + jwtSample + `","expire":1791981018368}`,
			expected: jwtSample,
		},
		{
			input:    `{"userToken":"` + jwtSample + `"}`,
			expected: jwtSample,
		},
		{
			input:    `"Bearer ` + jwtSample + `"`,
			expected: jwtSample,
		},
		{
			// Invalid tracking JSON must be rejected!
			input:    `{"attempts":1,"lastAttemptTimestamp":1791981018368}`,
			expected: "",
		},
		{
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		got := CleanToken(tt.input)
		if got != tt.expected {
			t.Errorf("CleanToken(%q) = %q; want %q", tt.input, got, tt.expected)
		}
	}
}
