package api

import (
	"encoding/json"
	"testing"

	"github.com/xiaopang/fusionapi/internal/model"
)

// --- buildFCCompatRequest ---

func TestBuildFCCompatRequest_NilInputs(t *testing.T) {
	_, err := buildFCCompatRequest(nil, nil)
	if err == nil {
		t.Fatal("expected error for nil inputs")
	}
}

func TestBuildFCCompatRequest_NoTools(t *testing.T) {
	orig := &model.ChatCompletionRequest{Model: "gpt-4", Messages: []model.Message{{Role: "user", Content: "hi"}}}
	trans := &model.ChatCompletionRequest{Model: "gpt-4", Messages: []model.Message{{Role: "user", Content: "hi"}}}

	_, err := buildFCCompatRequest(orig, trans)
	if err == nil {
		t.Fatal("expected error when no tools provided")
	}
}

func TestBuildFCCompatRequest_WithTools(t *testing.T) {
	orig := &model.ChatCompletionRequest{
		Model:    "gpt-4",
		Stream:   true,
		Messages: []model.Message{{Role: "user", Content: "what's the weather?"}},
		Tools: []model.Tool{
			{Type: "function", Function: model.Function{Name: "get_weather", Description: "Get weather", Parameters: map[string]any{"type": "object"}}},
		},
		ToolChoice: "auto",
	}
	trans := &model.ChatCompletionRequest{
		Model:    "gpt-4",
		Stream:   true,
		Messages: []model.Message{{Role: "user", Content: "what's the weather?"}},
		Tools: []model.Tool{
			{Type: "function", Function: model.Function{Name: "get_weather", Description: "Get weather", Parameters: map[string]any{"type": "object"}}},
		},
		ToolChoice: "auto",
	}

	result, err := buildFCCompatRequest(orig, trans)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Stream must be forced to false
	if result.Stream {
		t.Error("expected Stream=false in compat request")
	}
	// Tools/ToolChoice/Functions/FunctionCall must be stripped
	if len(result.Tools) != 0 {
		t.Error("expected Tools to be cleared")
	}
	if result.ToolChoice != nil {
		t.Error("expected ToolChoice to be nil")
	}
	if len(result.Functions) != 0 {
		t.Error("expected Functions to be cleared")
	}
	if result.FunctionCall != nil {
		t.Error("expected FunctionCall to be nil")
	}

	// First message must be the system prompt with tool schema
	if len(result.Messages) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(result.Messages))
	}
	if result.Messages[0].Role != "system" {
		t.Errorf("expected first message role=system, got %s", result.Messages[0].Role)
	}
	sysContent, ok := result.Messages[0].Content.(string)
	if !ok {
		t.Fatal("expected system message content to be string")
	}
	if len(sysContent) == 0 {
		t.Error("system prompt should not be empty")
	}
	// Should contain tool name
	if !contains(sysContent, "get_weather") {
		t.Error("system prompt should contain tool name 'get_weather'")
	}
}

func TestBuildFCCompatRequest_WithLegacyFunctions(t *testing.T) {
	orig := &model.ChatCompletionRequest{
		Model:    "gpt-4",
		Messages: []model.Message{{Role: "user", Content: "calculate"}},
		Functions: []model.Function{
			{Name: "calculate", Description: "Do math"},
		},
		FunctionCall: "auto",
	}
	trans := &model.ChatCompletionRequest{
		Model:    "gpt-4",
		Messages: []model.Message{{Role: "user", Content: "calculate"}},
	}

	result, err := buildFCCompatRequest(orig, trans)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sysContent := result.Messages[0].Content.(string)
	if !contains(sysContent, "calculate") {
		t.Error("system prompt should contain function name 'calculate'")
	}
}

// --- normalizeCompatMessages ---

func TestNormalizeCompatMessages_ToolRole(t *testing.T) {
	msgs := []model.Message{
		{Role: "tool", Content: "result data", ToolCallID: "call_123"},
	}

	result := normalizeCompatMessages(msgs)
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	if result[0].Role != "user" {
		t.Errorf("expected role=user, got %s", result[0].Role)
	}
	content, ok := result[0].Content.(string)
	if !ok {
		t.Fatal("expected string content")
	}
	if !contains(content, "call_123") {
		t.Error("should contain tool call ID")
	}
	if !contains(content, "result data") {
		t.Error("should contain original content")
	}
	// Should clear tool-specific fields
	if result[0].ToolCallID != "" {
		t.Error("ToolCallID should be cleared")
	}
}

func TestNormalizeCompatMessages_AssistantWithToolCalls(t *testing.T) {
	msgs := []model.Message{
		{
			Role:    "assistant",
			Content: "Let me check",
			ToolCalls: []model.ToolCall{
				{ID: "call_1", Type: "function", Function: model.FunctionCall{Name: "get_weather", Arguments: `{"city":"NYC"}`}},
			},
		},
	}

	result := normalizeCompatMessages(msgs)
	if result[0].Role != "assistant" {
		t.Errorf("expected role=assistant, got %s", result[0].Role)
	}
	content := result[0].Content.(string)
	if !contains(content, "get_weather") {
		t.Error("should contain tool call name")
	}
	if !contains(content, "NYC") {
		t.Error("should contain tool call arguments")
	}
	if len(result[0].ToolCalls) != 0 {
		t.Error("ToolCalls should be cleared")
	}
}

func TestNormalizeCompatMessages_AssistantWithFunctionCall(t *testing.T) {
	msgs := []model.Message{
		{
			Role:         "assistant",
			Content:      "",
			FunctionCall: &model.FunctionCall{Name: "calc", Arguments: `{"x":1}`},
		},
	}

	result := normalizeCompatMessages(msgs)
	content := result[0].Content.(string)
	if !contains(content, "calc") {
		t.Error("should contain function name")
	}
	if result[0].FunctionCall != nil {
		t.Error("FunctionCall should be cleared")
	}
}

func TestNormalizeCompatMessages_PlainMessages(t *testing.T) {
	msgs := []model.Message{
		{Role: "system", Content: "You are helpful"},
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi there"},
	}

	result := normalizeCompatMessages(msgs)
	if len(result) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(result))
	}
	for i, m := range result {
		if m.Role != msgs[i].Role {
			t.Errorf("message %d: expected role=%s, got %s", i, msgs[i].Role, m.Role)
		}
	}
}

// --- parseCompatOutput ---

func TestParseCompatOutput_ToolCall(t *testing.T) {
	input := `{"tool_call":{"name":"get_weather","arguments":{"city":"NYC"}}}`
	name, args, final, ok := parseCompatOutput(input)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if name != "get_weather" {
		t.Errorf("expected name=get_weather, got %s", name)
	}
	if final != "" {
		t.Errorf("expected empty final, got %s", final)
	}
	// args should be valid JSON
	if !json.Valid([]byte(args)) {
		t.Errorf("args should be valid JSON, got: %s", args)
	}
}

func TestParseCompatOutput_Final(t *testing.T) {
	input := `{"final":"The weather is sunny"}`
	name, _, final, ok := parseCompatOutput(input)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if name != "" {
		t.Errorf("expected empty name, got %s", name)
	}
	if final != "The weather is sunny" {
		t.Errorf("unexpected final: %s", final)
	}
}

func TestParseCompatOutput_CodeFence(t *testing.T) {
	input := "```json\n{\"tool_call\":{\"name\":\"search\",\"arguments\":{\"q\":\"test\"}}}\n```"
	name, _, _, ok := parseCompatOutput(input)
	if !ok {
		t.Fatal("expected ok=true for code-fenced output")
	}
	if name != "search" {
		t.Errorf("expected name=search, got %s", name)
	}
}

func TestParseCompatOutput_Empty(t *testing.T) {
	_, _, _, ok := parseCompatOutput("")
	if ok {
		t.Error("expected ok=false for empty input")
	}
}

func TestParseCompatOutput_InvalidJSON(t *testing.T) {
	_, _, _, ok := parseCompatOutput("not json at all")
	if ok {
		t.Error("expected ok=false for invalid JSON")
	}
}

func TestParseCompatOutput_ToolCallEmptyName(t *testing.T) {
	input := `{"tool_call":{"name":"","arguments":{}}}`
	name, _, _, ok := parseCompatOutput(input)
	// Empty name should not be treated as a valid tool call
	if ok && name != "" {
		t.Error("expected empty name to not be a valid tool call")
	}
}

func TestParseCompatOutput_ToolCallNullArgs(t *testing.T) {
	input := `{"tool_call":{"name":"do_thing","arguments":null}}`
	name, args, _, ok := parseCompatOutput(input)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if name != "do_thing" {
		t.Errorf("expected name=do_thing, got %s", name)
	}
	// null args should default to {}
	if args != "{}" {
		t.Errorf("expected args={}, got %s", args)
	}
}

// --- buildCompatResponse ---

func TestBuildCompatResponse_ToolCall(t *testing.T) {
	upstream := &model.ChatCompletionResponse{
		ID:      "chatcmpl-123",
		Object:  "chat.completion",
		Created: 1234567890,
		Model:   "gpt-4",
		Choices: []model.Choice{
			{
				Index: 0,
				Message: &model.Message{
					Role:    "assistant",
					Content: `{"tool_call":{"name":"get_weather","arguments":{"city":"NYC"}}}`,
				},
				FinishReason: "stop",
			},
		},
	}

	resp := buildCompatResponse(upstream)
	if len(resp.Choices) != 1 {
		t.Fatalf("expected 1 choice, got %d", len(resp.Choices))
	}
	if len(resp.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.Choices[0].Message.ToolCalls))
	}
	tc := resp.Choices[0].Message.ToolCalls[0]
	if tc.Function.Name != "get_weather" {
		t.Errorf("expected tool name=get_weather, got %s", tc.Function.Name)
	}
	if resp.Choices[0].FinishReason != "tool_calls" {
		t.Errorf("expected finish_reason=tool_calls, got %s", resp.Choices[0].FinishReason)
	}
}

func TestBuildCompatResponse_FinalText(t *testing.T) {
	upstream := &model.ChatCompletionResponse{
		ID:      "chatcmpl-456",
		Object:  "chat.completion",
		Created: 1234567890,
		Model:   "gpt-4",
		Choices: []model.Choice{
			{
				Index: 0,
				Message: &model.Message{
					Role:    "assistant",
					Content: `{"final":"The answer is 42"}`,
				},
				FinishReason: "stop",
			},
		},
	}

	resp := buildCompatResponse(upstream)
	content, ok := resp.Choices[0].Message.Content.(string)
	if !ok {
		t.Fatal("expected string content")
	}
	if content != "The answer is 42" {
		t.Errorf("expected 'The answer is 42', got '%s'", content)
	}
	if resp.Choices[0].FinishReason != "stop" {
		t.Errorf("expected finish_reason=stop, got %s", resp.Choices[0].FinishReason)
	}
}

func TestBuildCompatResponse_PlainText(t *testing.T) {
	upstream := &model.ChatCompletionResponse{
		ID:      "chatcmpl-789",
		Object:  "chat.completion",
		Created: 1234567890,
		Model:   "gpt-4",
		Choices: []model.Choice{
			{
				Index: 0,
				Message: &model.Message{
					Role:    "assistant",
					Content: "Just a plain response without JSON",
				},
				FinishReason: "stop",
			},
		},
	}

	resp := buildCompatResponse(upstream)
	content, ok := resp.Choices[0].Message.Content.(string)
	if !ok {
		t.Fatal("expected string content")
	}
	if content != "Just a plain response without JSON" {
		t.Errorf("unexpected content: %s", content)
	}
}

func TestBuildCompatResponse_EmptyResponse(t *testing.T) {
	upstream := &model.ChatCompletionResponse{
		Choices: []model.Choice{
			{Index: 0, Message: &model.Message{Role: "assistant", Content: ""}},
		},
	}

	resp := buildCompatResponse(upstream)
	content, ok := resp.Choices[0].Message.Content.(string)
	if !ok {
		t.Fatal("expected string content")
	}
	if content != "(empty response)" {
		t.Errorf("expected '(empty response)', got '%s'", content)
	}
}

// --- extractContentText ---

func TestExtractContentText_String(t *testing.T) {
	result := extractContentText("hello world")
	if result != "hello world" {
		t.Errorf("expected 'hello world', got '%s'", result)
	}
}

func TestExtractContentText_Parts(t *testing.T) {
	parts := []any{
		map[string]any{"type": "text", "text": "hello"},
		map[string]any{"type": "text", "text": "world"},
	}
	result := extractContentText(parts)
	if result != "hello\nworld" {
		t.Errorf("expected 'hello\\nworld', got '%s'", result)
	}
}

func TestExtractContentText_Nil(t *testing.T) {
	result := extractContentText(nil)
	if result != "null" {
		// json.Marshal(nil) = "null"
		t.Logf("nil content returned: '%s'", result)
	}
}

// --- stripCodeFence ---

func TestStripCodeFence_NoFence(t *testing.T) {
	result := stripCodeFence(`{"key": "value"}`)
	if result != `{"key": "value"}` {
		t.Errorf("unexpected: %s", result)
	}
}

func TestStripCodeFence_JSONFence(t *testing.T) {
	input := "```json\n{\"key\": \"value\"}\n```"
	result := stripCodeFence(input)
	expected := `{"key": "value"}`
	if result != expected {
		t.Errorf("expected '%s', got '%s'", expected, result)
	}
}

func TestStripCodeFence_PlainFence(t *testing.T) {
	input := "```\n{\"key\": \"value\"}\n```"
	result := stripCodeFence(input)
	expected := `{"key": "value"}`
	if result != expected {
		t.Errorf("expected '%s', got '%s'", expected, result)
	}
}

// --- sourceSupportsFC ---

func TestSourceSupportsFC_NilSource(t *testing.T) {
	if sourceSupportsFC(nil, "gpt-4") {
		t.Error("expected false for nil source")
	}
}

func TestSourceSupportsFC_NonCPA(t *testing.T) {
	src := &model.Source{
		Type:         model.SourceTypeOpenAI,
		Capabilities: model.Capabilities{FunctionCalling: true},
	}
	if !sourceSupportsFC(src, "gpt-4") {
		t.Error("expected true for OpenAI with FC capability")
	}

	src.Capabilities.FunctionCalling = false
	if sourceSupportsFC(src, "gpt-4") {
		t.Error("expected false for OpenAI without FC capability")
	}
}

// --- helper ---

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
