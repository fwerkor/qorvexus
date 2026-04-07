package types

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type ContentPart struct {
	Type     string `json:"type" yaml:"type"`
	Text     string `json:"text,omitempty" yaml:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty" yaml:"image_url,omitempty"`
	MimeType string `json:"mime_type,omitempty" yaml:"mime_type,omitempty"`
}

type ToolCall struct {
	ID        string `json:"id" yaml:"id"`
	Name      string `json:"name" yaml:"name"`
	Arguments string `json:"arguments" yaml:"arguments"`
}

type Message struct {
	Role       Role          `json:"role" yaml:"role"`
	Content    string        `json:"content,omitempty" yaml:"content,omitempty"`
	Parts      []ContentPart `json:"parts,omitempty" yaml:"parts,omitempty"`
	Name       string        `json:"name,omitempty" yaml:"name,omitempty"`
	ToolCallID string        `json:"tool_call_id,omitempty" yaml:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall    `json:"tool_calls,omitempty" yaml:"tool_calls,omitempty"`
}

type ToolDefinition struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
}

type ToolResult struct {
	Name    string `json:"name"`
	CallID  string `json:"call_id"`
	Content string `json:"content"`
	Error   bool   `json:"error"`
}

type TrustLevel string

const (
	TrustOwner    TrustLevel = "owner"
	TrustTrusted  TrustLevel = "trusted"
	TrustExternal TrustLevel = "external"
	TrustSystem   TrustLevel = "system"
)

type ConversationContext struct {
	Channel        string     `json:"channel,omitempty" yaml:"channel,omitempty"`
	ThreadID       string     `json:"thread_id,omitempty" yaml:"thread_id,omitempty"`
	SenderID       string     `json:"sender_id,omitempty" yaml:"sender_id,omitempty"`
	SenderName     string     `json:"sender_name,omitempty" yaml:"sender_name,omitempty"`
	Trust          TrustLevel `json:"trust,omitempty" yaml:"trust,omitempty"`
	IsOwner        bool       `json:"is_owner,omitempty" yaml:"is_owner,omitempty"`
	ReplyAsAgent   bool       `json:"reply_as_agent,omitempty" yaml:"reply_as_agent,omitempty"`
	WorkingForUser bool       `json:"working_for_user,omitempty" yaml:"working_for_user,omitempty"`
}
