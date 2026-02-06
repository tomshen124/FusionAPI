package model

import "encoding/json"

// ChatCompletionRequest OpenAI 兼容的聊天补全请求
type ChatCompletionRequest struct {
	Model            string          `json:"model"`
	Messages         []Message       `json:"messages"`
	Temperature      *float64        `json:"temperature,omitempty"`
	TopP             *float64        `json:"top_p,omitempty"`
	N                *int            `json:"n,omitempty"`
	Stream           bool            `json:"stream,omitempty"`
	Stop             json.RawMessage `json:"stop,omitempty"`
	MaxTokens        *int            `json:"max_tokens,omitempty"`
	PresencePenalty  *float64        `json:"presence_penalty,omitempty"`
	FrequencyPenalty *float64        `json:"frequency_penalty,omitempty"`
	LogitBias        map[string]int  `json:"logit_bias,omitempty"`
	User             string          `json:"user,omitempty"`

	// Function Calling
	Tools      []Tool `json:"tools,omitempty"`
	ToolChoice any    `json:"tool_choice,omitempty"`

	// Legacy function calling (deprecated but still supported)
	Functions        []Function `json:"functions,omitempty"`
	FunctionCall     any        `json:"function_call,omitempty"`

	// Extended Thinking (Claude)
	Thinking *ThinkingConfig `json:"thinking,omitempty"`

	// Response format
	ResponseFormat *ResponseFormat `json:"response_format,omitempty"`
}

// Message 消息
type Message struct {
	Role       string     `json:"role"`
	Content    any        `json:"content"` // string 或 []ContentPart
	Name       string     `json:"name,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`

	// Legacy function calling
	FunctionCall *FunctionCall `json:"function_call,omitempty"`
}

// ContentPart 多模态内容部分
type ContentPart struct {
	Type     string    `json:"type"` // "text" or "image_url"
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

// ImageURL 图片URL
type ImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"` // "auto", "low", "high"
}

// Tool 工具定义
type Tool struct {
	Type     string   `json:"type"` // "function"
	Function Function `json:"function"`
}

// Function 函数定义
type Function struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// ToolCall 工具调用
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"` // "function"
	Function FunctionCall `json:"function"`
}

// FunctionCall 函数调用
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ThinkingConfig Extended Thinking 配置
type ThinkingConfig struct {
	Type         string `json:"type,omitempty"`          // "enabled"
	BudgetTokens int    `json:"budget_tokens,omitempty"` // thinking token 预算
}

// ResponseFormat 响应格式
type ResponseFormat struct {
	Type string `json:"type"` // "text" or "json_object"
}

// ChatCompletionResponse OpenAI 兼容的聊天补全响应
type ChatCompletionResponse struct {
	ID                string   `json:"id"`
	Object            string   `json:"object"`
	Created           int64    `json:"created"`
	Model             string   `json:"model"`
	Choices           []Choice `json:"choices"`
	Usage             *Usage   `json:"usage,omitempty"`
	SystemFingerprint string   `json:"system_fingerprint,omitempty"`
}

// Choice 选项
type Choice struct {
	Index        int      `json:"index"`
	Message      *Message `json:"message,omitempty"`
	Delta        *Message `json:"delta,omitempty"` // 流式响应
	FinishReason string   `json:"finish_reason,omitempty"`
}

// Usage Token 使用量
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// StreamChunk SSE 流式响应块
type StreamChunk struct {
	ID                string   `json:"id"`
	Object            string   `json:"object"`
	Created           int64    `json:"created"`
	Model             string   `json:"model"`
	Choices           []Choice `json:"choices"`
	SystemFingerprint string   `json:"system_fingerprint,omitempty"`
}

// ModelsResponse 模型列表响应
type ModelsResponse struct {
	Object string       `json:"object"`
	Data   []ModelInfo  `json:"data"`
}

// ModelInfo 模型信息
type ModelInfo struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// ErrorResponse 错误响应
type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail 错误详情
type ErrorDetail struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Param   string `json:"param,omitempty"`
	Code    string `json:"code,omitempty"`
}

// HasTools 检查请求是否包含工具调用
func (r *ChatCompletionRequest) HasTools() bool {
	return len(r.Tools) > 0 || len(r.Functions) > 0
}

// HasThinking 检查请求是否启用了 Extended Thinking
func (r *ChatCompletionRequest) HasThinking() bool {
	return r.Thinking != nil && r.Thinking.Type == "enabled"
}

// HasVision 检查请求是否包含图片
func (r *ChatCompletionRequest) HasVision() bool {
	for _, msg := range r.Messages {
		if parts, ok := msg.Content.([]any); ok {
			for _, part := range parts {
				if p, ok := part.(map[string]any); ok {
					if p["type"] == "image_url" {
						return true
					}
				}
			}
		}
	}
	return false
}
