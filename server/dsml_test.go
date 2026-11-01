package server

import (
	"testing"
)

func TestParseToolCallsDSML(t *testing.T) {
	rawInput := `<｜｜DSML｜｜ calls>
<｜｜DSML｜｜ invoke name="Bash">
<｜｜DSML｜｜ parameter name="command" string="true">ls -la "/tmp/project"</｜｜DSML｜｜ parameter>
<｜｜DSML｜｜ parameter name="description" string="true">List project directory contents</｜｜DSML｜｜ parameter>
</｜｜DSML｜｜ invoke>
</｜｜DSML｜｜ calls>`

	res := ParseToolCalls(rawInput)
	if !res.HasTools {
		t.Fatalf("expected HasTools to be true")
	}
	if len(res.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(res.ToolCalls))
	}

	tc := res.ToolCalls[0]
	if tc.Name != "Bash" {
		t.Errorf("expected tool name 'Bash', got '%s'", tc.Name)
	}

	cmd, ok := tc.Arguments["command"].(string)
	if !ok || cmd != `ls -la "/tmp/project"` {
		t.Errorf("unexpected command argument: %v", tc.Arguments["command"])
	}

	desc, ok := tc.Arguments["description"].(string)
	if !ok || desc != "List project directory contents" {
		t.Errorf("unexpected description argument: %v", tc.Arguments["description"])
	}
}

func TestParseToolCallsSinglePipe(t *testing.T) {
	rawInput := `I'll create a Python wrapper script that shells out to the compiled Go binary. Let me look at the CLI's commands and how it's built so the wrapper matches.

<｜｜DSML｜｜ calls>
<｜DSML｜｜ invoke name="Bash">
<｜｜DSML｜｜ parameter name="command" string="true">cd "/tmp/project" && echo "=== cmd/ ===" && find cmd -type f && echo "=== bin/ ===" && ls -la bin/ && echo "=== go.mod ===" && cat go.mod && echo "=== .env ===" && cat .env && echo "=== main.go switches ===" && grep -n "case \"" main.go</｜｜DSML｜｜ parameter>
<｜｜DSML｜｜ parameter name="description" string="true">Inspect cmd/, bin/, go.mod, and main.go command cases</｜｜DSML｜｜ parameter>
</｜｜DSML｜｜ invoke>
</｜｜DSML｜｜ calls>`

	res := ParseToolCalls(rawInput)
	if !res.HasTools {
		t.Fatalf("expected HasTools to be true")
	}
	if len(res.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(res.ToolCalls))
	}

	tc := res.ToolCalls[0]
	if tc.Name != "Bash" {
		t.Errorf("expected Bash, got %s", tc.Name)
	}
	if tc.Arguments["description"] != "Inspect cmd/, bin/, go.mod, and main.go command cases" {
		t.Errorf("unexpected description: %v", tc.Arguments["description"])
	}
}

func TestParseToolCallsMixed(t *testing.T) {
	rawInput := `Albatta, fayllarni tekshiraman.
<｜｜DSML｜｜ calls>
<｜｜DSML｜｜ invoke name="FileWrite">
<｜｜DSML｜｜ parameter name="path" string="true">main.py</｜｜DSML｜｜ parameter>
<｜｜DSML｜｜ parameter name="content" string="true">print("Hello World")</｜｜DSML｜｜ parameter>
</｜｜DSML｜｜ invoke>
</｜｜DSML｜｜ calls>`

	res := ParseToolCalls(rawInput)
	if !res.HasTools {
		t.Fatalf("expected HasTools to be true")
	}
	if res.CleanText != "Albatta, fayllarni tekshiraman." {
		t.Errorf("unexpected CleanText: %q", res.CleanText)
	}
	if len(res.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(res.ToolCalls))
	}
	if res.ToolCalls[0].Name != "FileWrite" {
		t.Errorf("expected FileWrite, got %s", res.ToolCalls[0].Name)
	}
	if res.ToolCalls[0].Arguments["path"] != "main.py" {
		t.Errorf("expected main.py, got %v", res.ToolCalls[0].Arguments["path"])
	}
}

func TestParseToolCallsJSON(t *testing.T) {
	rawInput := `{"tool_calls": [{"name": "Bash", "arguments": {"command": "git status"}}]}`

	res := ParseToolCalls(rawInput)
	if !res.HasTools {
		t.Fatalf("expected HasTools to be true")
	}
	if len(res.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(res.ToolCalls))
	}
	if res.ToolCalls[0].Name != "Bash" {
		t.Errorf("expected Bash, got %s", res.ToolCalls[0].Name)
	}
	if res.ToolCalls[0].Arguments["command"] != "git status" {
		t.Errorf("expected git status, got %v", res.ToolCalls[0].Arguments["command"])
	}
}
