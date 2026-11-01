package server

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCleanJSON(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:    "pure valid json object",
			input:   `{"status": "ok", "count": 42}`,
			want:    `{"count":42,"status":"ok"}`,
			wantErr: false,
		},
		{
			name:    "markdown wrapped json",
			input:   "Here is your result:\n```json\n{\"answer\": 100}\n```\nHope that helps!",
			want:    "{\"answer\": 100}",
			wantErr: false,
		},
		{
			name:    "json array wrapped in codeblock",
			input:   "```\n[1, 2, 3]\n```",
			want:    "[1, 2, 3]",
			wantErr: false,
		},
		{
			name:    "surrounding text without backticks",
			input:   "The requested data is {\"key\": \"value\"} as requested.",
			want:    "{\"key\": \"value\"}",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CleanJSON(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("CleanJSON() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && !json.Valid([]byte(got)) {
				t.Errorf("CleanJSON() got invalid json: %s", got)
			}
		})
	}
}

func TestBuildJSONInstruction(t *testing.T) {
	// 1. Literal "json"
	inst1 := BuildJSONInstruction(json.RawMessage(`"json"`))
	if !strings.Contains(inst1, "valid JSON") {
		t.Errorf("Expected JSON requirement in instruction, got: %s", inst1)
	}

	// 2. JSON Schema object
	schema := `{"type": "object", "properties": {"name": {"type": "string"}}}`
	inst2 := BuildJSONInstruction(json.RawMessage(schema))
	if !strings.Contains(inst2, "JSON Schema") || !strings.Contains(inst2, "name") {
		t.Errorf("Expected schema inclusion, got: %s", inst2)
	}

	// 3. Empty format
	inst3 := BuildJSONInstruction(nil)
	if inst3 != "" {
		t.Errorf("Expected empty string for nil format, got: %s", inst3)
	}
}
