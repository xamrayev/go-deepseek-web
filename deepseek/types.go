package deepseek

import (
	"strings"
	"time"
)

// Model types supported by DeepSeek Web API.
const (
	ModelChat           = "deepseek-chat"
	ModelV3             = "deepseek-v3"
	ModelDefault        = "deepseek-default"
	ModelReasoner       = "deepseek-reasoner"
	ModelR1             = "deepseek-r1"
	ModelChatSearch     = "deepseek-chat-search"
	ModelDefaultSearch  = "deepseek-default-search"
	ModelReasonerSearch = "deepseek-reasoner-search"
	ModelR1Search       = "deepseek-r1-search"
	ModelExpert         = "deepseek-expert"
	ModelV4Pro          = "deepseek-v4-pro"
)

// ModelCapabilities defines the capability flags for a model.
type ModelCapabilities struct {
	Reasoning bool `json:"reasoning"`
	WebSearch bool `json:"web_search"`
	Files     bool `json:"files"`
}

// ModelConfig contains internal DeepSeek parameters for a model ID.
type ModelConfig struct {
	ID                string            `json:"id"`
	RealModel         string            `json:"real_model"`
	ModelType         string            `json:"model_type"` // "default" or "expert"
	ThinkingEnabled   bool              `json:"thinking_enabled"`
	SearchEnabled     bool              `json:"search_enabled"`
	Capabilities      ModelCapabilities `json:"capabilities"`
	Supported         bool              `json:"supported"`
	UnavailableReason string            `json:"unavailable_reason,omitempty"`
}

// ModelConfigs is the registry of supported and aliased models.
var ModelConfigs = map[string]ModelConfig{
	ModelChat: {
		ID:              ModelChat,
		RealModel:       "DeepSeek-V3 Chat (Web Default)",
		ModelType:       "default",
		ThinkingEnabled: false,
		SearchEnabled:   false,
		Capabilities:    ModelCapabilities{Reasoning: false, WebSearch: false, Files: true},
		Supported:       true,
	},
	ModelV3: {
		ID:              ModelV3,
		RealModel:       "DeepSeek-V3 Chat (Web Default)",
		ModelType:       "default",
		ThinkingEnabled: false,
		SearchEnabled:   false,
		Capabilities:    ModelCapabilities{Reasoning: false, WebSearch: false, Files: true},
		Supported:       true,
	},
	ModelDefault: {
		ID:              ModelDefault,
		RealModel:       "DeepSeek-V3 Chat (Web Default)",
		ModelType:       "default",
		ThinkingEnabled: false,
		SearchEnabled:   false,
		Capabilities:    ModelCapabilities{Reasoning: false, WebSearch: false, Files: true},
		Supported:       true,
	},
	ModelReasoner: {
		ID:              ModelReasoner,
		RealModel:       "DeepSeek-R1 Reasoner (Web Default + Thinking)",
		ModelType:       "default",
		ThinkingEnabled: true,
		SearchEnabled:   false,
		Capabilities:    ModelCapabilities{Reasoning: true, WebSearch: false, Files: true},
		Supported:       true,
	},
	ModelR1: {
		ID:              ModelR1,
		RealModel:       "DeepSeek-R1 Reasoner (Web Default + Thinking)",
		ModelType:       "default",
		ThinkingEnabled: true,
		SearchEnabled:   false,
		Capabilities:    ModelCapabilities{Reasoning: true, WebSearch: false, Files: true},
		Supported:       true,
	},
	ModelChatSearch: {
		ID:              ModelChatSearch,
		RealModel:       "DeepSeek-V3 Chat + Web Search",
		ModelType:       "default",
		ThinkingEnabled: false,
		SearchEnabled:   true,
		Capabilities:    ModelCapabilities{Reasoning: false, WebSearch: true, Files: true},
		Supported:       true,
	},
	ModelReasonerSearch: {
		ID:              ModelReasonerSearch,
		RealModel:       "DeepSeek-R1 Reasoner + Web Search",
		ModelType:       "default",
		ThinkingEnabled: true,
		SearchEnabled:   true,
		Capabilities:    ModelCapabilities{Reasoning: true, WebSearch: true, Files: true},
		Supported:       true,
	},
	ModelR1Search: {
		ID:              ModelR1Search,
		RealModel:       "DeepSeek-R1 Reasoner + Web Search",
		ModelType:       "default",
		ThinkingEnabled: true,
		SearchEnabled:   true,
		Capabilities:    ModelCapabilities{Reasoning: true, WebSearch: true, Files: true},
		Supported:       true,
	},
	ModelExpert: {
		ID:              ModelExpert,
		RealModel:       "DeepSeek Web Expert",
		ModelType:       "expert",
		ThinkingEnabled: false,
		SearchEnabled:   false,
		Capabilities:    ModelCapabilities{Reasoning: false, WebSearch: false, Files: false},
		Supported:       true,
	},
	ModelV4Pro: {
		ID:              ModelV4Pro,
		RealModel:       "DeepSeek Web Expert + Thinking",
		ModelType:       "expert",
		ThinkingEnabled: true,
		SearchEnabled:   false,
		Capabilities:    ModelCapabilities{Reasoning: true, WebSearch: false, Files: false},
		Supported:       true,
	},
}

// ResolveModelConfig looks up model configuration with fallback to deepseek-reasoner or deepseek-chat.
func ResolveModelConfig(model string) ModelConfig {
	m := strings.ToLower(strings.TrimSpace(model))
	if cfg, ok := ModelConfigs[m]; ok {
		return cfg
	}

	// Direct Aliases
	switch m {
	case "r1", "reasoner", "deepseek-r1", "deepseek-reasoner", "r1-thinking", "thinking":
		return ModelConfigs[ModelReasoner]
	case "v3", "chat", "deepseek-v3", "deepseek-chat", "fast":
		return ModelConfigs[ModelChat]
	case "r1-search", "reasoner-search", "deepseek-r1-search", "deepseek-reasoner-search":
		return ModelConfigs[ModelReasonerSearch]
	case "v3-search", "chat-search", "deepseek-v3-search", "deepseek-chat-search":
		return ModelConfigs[ModelChatSearch]
	case "expert":
		return ModelConfigs[ModelExpert]
	case "v4pro", "v4-pro", "expert-thinking":
		return ModelConfigs[ModelV4Pro]
	}

	// Smart Claude & OpenAI alias matching
	if strings.Contains(m, "sonnet") || strings.Contains(m, "opus") || strings.Contains(m, "reasoner") || strings.Contains(m, "r1") || strings.Contains(m, "o1") || strings.Contains(m, "o3") {
		return ModelConfigs[ModelReasoner]
	}
	if strings.Contains(m, "haiku") || strings.Contains(m, "chat") || strings.Contains(m, "v3") || strings.Contains(m, "mini") || strings.Contains(m, "flash") {
		return ModelConfigs[ModelChat]
	}

	// Case-insensitive lookup
	for k, cfg := range ModelConfigs {
		if stringsEqualFold(k, m) {
			return cfg
		}
	}
	return ModelConfigs[ModelReasoner]
}

func stringsEqualFold(s1, s2 string) bool {
	if len(s1) != len(s2) {
		return false
	}
	for i := 0; i < len(s1); i++ {
		c1, c2 := s1[i], s2[i]
		if c1 >= 'A' && c1 <= 'Z' {
			c1 += 'a' - 'A'
		}
		if c2 >= 'A' && c2 <= 'Z' {
			c2 += 'a' - 'A'
		}
		if c1 != c2 {
			return false
		}
	}
	return true
}

// UserProfile represents user account info from /api/v0/users/current.
type UserProfile struct {
	ID        string                 `json:"id"`
	Email     string                 `json:"email"`
	Phone     string                 `json:"phone"`
	Nickname  string                 `json:"nickname"`
	AvatarURL string                 `json:"avatar_url"`
	RawData   map[string]interface{} `json:"raw_data,omitempty"`
}

// ChatChunk is an incremental piece of response text or reasoning.
type ChatChunk struct {
	Content          string `json:"content"`
	ReasoningContent string `json:"reasoning_content"`
	SessionID        string `json:"session_id,omitempty"`
	MessageID        string `json:"message_id,omitempty"`
	FinishReason     string `json:"finish_reason,omitempty"`
}

// ChatResponse contains the full aggregated response.
type ChatResponse struct {
	Content          string `json:"content"`
	ReasoningContent string `json:"reasoning_content"`
	SessionID        string `json:"session_id"`
	MessageID        string `json:"message_id"`
	Model            string `json:"model"`
}

// AuthData represents persisted authentication credentials.
type AuthData struct {
	Token        string `json:"token"`
	Cookie       string `json:"cookie,omitempty"`
	HifDliq      string `json:"hif_dliq,omitempty"`
	HifLeim      string `json:"hif_leim,omitempty"`
	WasmURL      string `json:"wasmUrl,omitempty"`
	BaseURL      string `json:"baseUrl,omitempty"`
	CookiesCount int    `json:"cookiesCount,omitempty"`
}

// ChatMessage represents a message in a conversation.
type ChatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolCall represents an OpenAI or Anthropic tool call.
type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function ToolCallFunction `json:"function"`
}

// ToolCallFunction contains the name and JSON string arguments of a tool.
type ToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// AgentSession tracks active conversation state.
type AgentSession struct {
	ID              string
	ParentMessageID *int64
	CreatedAt       time.Time
	LastActivityAt  time.Time
	MessageCount    int
	History         []ChatMessage
	ModelOverride   string
}
