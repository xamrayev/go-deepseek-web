package server

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// ParsedToolCall represents an extracted tool invocation.
type ParsedToolCall struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// ParseResult contains cleaned text and any tool calls found in the output.
type ParseResult struct {
	CleanText string
	ToolCalls []ParsedToolCall
	HasTools  bool
}

var (
	// Match DSML calls block: <||DSML|| calls> or <｜｜DSML｜｜ calls> ... </...calls>
	// Uses (?:\||｜)+ to handle single or multiple fullwidth or ASCII pipe characters.
	dsmlBlockRegex = regexp.MustCompile(`(?s)<\s*(?:\||｜)+\s*DSML\s*(?:\||｜)+\s*calls\s*>(.*?)(?:<\s*/\s*(?:\||｜)+\s*DSML\s*(?:\||｜)+\s*calls\s*>|$)`)

	// Match each invoke: <||DSML|| invoke name="ToolName"> ... </||DSML|| invoke>
	dsmlInvokeRegex = regexp.MustCompile(`(?s)<\s*(?:\||｜)+\s*DSML\s*(?:\||｜)+\s*invoke\s+name=["']([^"']+)["'][^>]*>(.*?)(?:<\s*/\s*(?:\||｜)+\s*DSML\s*(?:\||｜)+\s*invoke\s*>|$)`)

	// Match each parameter: <||DSML|| parameter name="paramName" ...>val</||DSML|| parameter>
	dsmlParamRegex = regexp.MustCompile(`(?s)<\s*(?:\||｜)+\s*DSML\s*(?:\||｜)+\s*parameter\s+name=["']([^"']+)["']([^>]*)>(.*?)(?:<\s*/\s*(?:\||｜)+\s*DSML\s*(?:\||｜)+\s*parameter\s*>|$)`)

	// Match alternate tool calls: <|tool_calls|> or <|tool calls|> ... </|tool_calls|>
	altToolBlockRegex = regexp.MustCompile(`(?s)<\s*(?:\||｜)+\s*tool_?calls\s*(?:\||｜)+>(.*?)(?:<\s*/\s*(?:\||｜)+\s*tool_?calls\s*(?:\||｜)+>|$)`)

	// Match JSON tool_calls block: ```json {"tool_calls": [...]} ``` or raw {"tool_calls": [...]}
	jsonToolCallsRegex = regexp.MustCompile(`(?s)\{\s*"tool_calls"\s*:\s*\[.*?\]\s*\}`)
)

// ParseToolCalls extracts tool invocations from DeepSeek response text.
func ParseToolCalls(rawText string) ParseResult {
	if rawText == "" {
		return ParseResult{}
	}

	var toolCalls []ParsedToolCall

	// 1. Check for standard DSML format
	dsmlMatches := dsmlBlockRegex.FindAllStringSubmatchIndex(rawText, -1)
	if len(dsmlMatches) > 0 {
		var cleanBuilder strings.Builder
		lastIndex := 0

		for _, match := range dsmlMatches {
			start, end := match[0], match[1]
			cleanBuilder.WriteString(rawText[lastIndex:start])
			lastIndex = end

			innerContent := rawText[match[2]:match[3]]
			invokes := dsmlInvokeRegex.FindAllStringSubmatch(innerContent, -1)
			for idx, inv := range invokes {
				toolName := strings.TrimSpace(inv[1])
				paramContent := inv[2]
				args := make(map[string]any)

				params := dsmlParamRegex.FindAllStringSubmatch(paramContent, -1)
				for _, p := range params {
					pName := strings.TrimSpace(p[1])
					pAttrs := p[2]
					pValue := strings.TrimSpace(p[3])

					isExplicitString := strings.Contains(pAttrs, `string="true"`) || strings.Contains(pAttrs, `string='true'`)
					if isExplicitString {
						args[pName] = pValue
					} else {
						// Attempt JSON unmarshal for numbers, booleans, objects, arrays
						var jsonVal any
						if err := json.Unmarshal([]byte(pValue), &jsonVal); err == nil {
							args[pName] = jsonVal
						} else {
							args[pName] = pValue
						}
					}
				}

				toolID := fmt.Sprintf("toolu_%d_%d", time.Now().UnixNano()%10000000, idx)
				toolCalls = append(toolCalls, ParsedToolCall{
					ID:        toolID,
					Name:      toolName,
					Arguments: args,
				})
			}
		}

		if lastIndex < len(rawText) {
			cleanBuilder.WriteString(rawText[lastIndex:])
		}

		return ParseResult{
			CleanText: strings.TrimSpace(cleanBuilder.String()),
			ToolCalls: toolCalls,
			HasTools:  len(toolCalls) > 0,
		}
	}

	// 2. Check for alternate <|tool_calls|> format
	altMatches := altToolBlockRegex.FindAllStringSubmatchIndex(rawText, -1)
	if len(altMatches) > 0 {
		var cleanBuilder strings.Builder
		lastIndex := 0

		for _, match := range altMatches {
			start, end := match[0], match[1]
			cleanBuilder.WriteString(rawText[lastIndex:start])
			lastIndex = end

			innerContent := strings.TrimSpace(rawText[match[2]:match[3]])
			// Try parsing innerContent as JSON array of tool calls
			var calls []struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			if err := json.Unmarshal([]byte(innerContent), &calls); err == nil {
				for idx, c := range calls {
					toolID := fmt.Sprintf("toolu_%d_%d", time.Now().UnixNano()%10000000, idx)
					toolCalls = append(toolCalls, ParsedToolCall{
						ID:        toolID,
						Name:      c.Name,
						Arguments: c.Arguments,
					})
				}
			}
		}

		if lastIndex < len(rawText) {
			cleanBuilder.WriteString(rawText[lastIndex:])
		}

		if len(toolCalls) > 0 {
			return ParseResult{
				CleanText: strings.TrimSpace(cleanBuilder.String()),
				ToolCalls: toolCalls,
				HasTools:  true,
			}
		}
	}

	// 3. Check for JSON {"tool_calls": [...]} format
	jsonLoc := jsonToolCallsRegex.FindStringIndex(rawText)
	if len(jsonLoc) == 2 {
		jsonSnippet := rawText[jsonLoc[0]:jsonLoc[1]]
		var parsed struct {
			ToolCalls []struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
				Function  *struct {
					Name      string `json:"name"`
					Arguments any    `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		}

		if err := json.Unmarshal([]byte(jsonSnippet), &parsed); err == nil && len(parsed.ToolCalls) > 0 {
			cleanText := strings.TrimSpace(rawText[:jsonLoc[0]] + " " + rawText[jsonLoc[1]:])
			for idx, tc := range parsed.ToolCalls {
				tName := tc.Name
				tArgs := tc.Arguments

				if tc.Function != nil {
					if tName == "" {
						tName = tc.Function.Name
					}
					if tArgs == nil {
						switch a := tc.Function.Arguments.(type) {
						case string:
							_ = json.Unmarshal([]byte(a), &tArgs)
						case map[string]any:
							tArgs = a
						}
					}
				}

				if tArgs == nil {
					tArgs = make(map[string]any)
				}

				toolID := fmt.Sprintf("toolu_%d_%d", time.Now().UnixNano()%10000000, idx)
				toolCalls = append(toolCalls, ParsedToolCall{
					ID:        toolID,
					Name:      tName,
					Arguments: tArgs,
				})
			}

			return ParseResult{
				CleanText: cleanText,
				ToolCalls: toolCalls,
				HasTools:  true,
			}
		}
	}

	return ParseResult{
		CleanText: rawText,
		ToolCalls: nil,
		HasTools:  false,
	}
}
