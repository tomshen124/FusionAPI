package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaopang/fusionapi/internal/model"
)

type fcCompatPayload struct {
	ToolCall *fcCompatToolCall `json:"tool_call"`
	Final    string            `json:"final"`
}

type fcCompatToolCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func sourceSupportsFC(src *model.Source, modelName string) bool {
	if src == nil {
		return false
	}
	if src.Type == model.SourceTypeCPA {
		return src.SupportsFCForModel(modelName)
	}
	return src.Capabilities.FunctionCalling
}

func (h *ProxyHandler) handleFCCompatRequest(c *gin.Context, originalReq, translatedReq *model.ChatCompletionRequest, src *model.Source, startTime time.Time, failoverFrom string, clientInfo *model.ClientInfo) bool {
	compatReq, err := buildFCCompatRequest(originalReq, translatedReq)
	if err != nil {
		return false
	}

	upstreamResp, err := h.sendChatRequest(c, compatReq, src)
	if err != nil {
		h.updateSourceLatency(src, time.Since(startTime), err)
		return false
	}

	compatResp := buildCompatResponse(upstreamResp)

	h.updateSourceLatency(src, time.Since(startTime), nil)
	h.logRequest(originalReq, compatResp, src, startTime, http.StatusOK, nil, failoverFrom, clientInfo, true)

	if originalReq.Stream {
		writeCompatStreamResponse(c, compatResp)
	} else {
		c.JSON(http.StatusOK, compatResp)
	}

	return true
}

func (h *ProxyHandler) sendChatRequest(c *gin.Context, req *model.ChatCompletionRequest, src *model.Source) (*model.ChatCompletionResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(c.Request.Context(), "POST", src.BaseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	h.setHeaders(httpReq, src)

	resp, err := h.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var chatResp model.ChatCompletionResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, err
	}
	return &chatResp, nil
}

func buildFCCompatRequest(originalReq, translatedReq *model.ChatCompletionRequest) (*model.ChatCompletionRequest, error) {
	if originalReq == nil || translatedReq == nil {
		return nil, fmt.Errorf("invalid request")
	}

	tools := collectCompatTools(originalReq)
	if len(tools) == 0 {
		return nil, fmt.Errorf("no tools to compat")
	}

	compatReq := *translatedReq
	compatReq.Stream = false
	compatReq.Tools = nil
	compatReq.Functions = nil
	compatReq.ToolChoice = nil
	compatReq.FunctionCall = nil

	normalized := normalizeCompatMessages(compatReq.Messages)
	compatReq.Messages = append([]model.Message{
		{
			Role:    "system",
			Content: buildFCCompatSystemPrompt(tools),
		},
	}, normalized...)

	return &compatReq, nil
}

func collectCompatTools(req *model.ChatCompletionRequest) []model.Tool {
	if len(req.Tools) > 0 {
		return req.Tools
	}

	tools := make([]model.Tool, 0, len(req.Functions))
	for _, fn := range req.Functions {
		tools = append(tools, model.Tool{
			Type:     "function",
			Function: fn,
		})
	}
	return tools
}

func buildFCCompatSystemPrompt(tools []model.Tool) string {
	toolJSON, _ := json.Marshal(tools)

	return "You are FusionAPI function-calling compatibility layer.\n" +
		"The upstream model does not support native tools/function_call.\n" +
		"Available tools(JSON schema):\n" + string(toolJSON) + "\n" +
		"Return ONLY one JSON object, no markdown/code fence.\n" +
		"If a tool should be called, output:\n" +
		"{\"tool_call\":{\"name\":\"<tool_name>\",\"arguments\":{...}}}\n" +
		"If no tool call is needed, output:\n" +
		"{\"final\":\"<assistant_response>\"}"
}

func normalizeCompatMessages(messages []model.Message) []model.Message {
	normalized := make([]model.Message, 0, len(messages))

	for _, msg := range messages {
		m := msg

		if m.Role == "tool" {
			content := extractContentText(m.Content)
			if m.ToolCallID != "" {
				content = fmt.Sprintf("Tool result (%s): %s", m.ToolCallID, content)
			} else {
				content = "Tool result: " + content
			}
			m.Role = "user"
			m.Content = content
			m.ToolCallID = ""
			m.Name = ""
			m.ToolCalls = nil
			m.FunctionCall = nil
		}

		if m.Role == "assistant" && (len(m.ToolCalls) > 0 || m.FunctionCall != nil) {
			text := extractContentText(m.Content)
			if text != "" {
				text += "\n"
			}
			for _, tc := range m.ToolCalls {
				text += fmt.Sprintf("Assistant tool call: name=%s arguments=%s\n", tc.Function.Name, tc.Function.Arguments)
			}
			if m.FunctionCall != nil {
				text += fmt.Sprintf("Assistant function call: name=%s arguments=%s", m.FunctionCall.Name, m.FunctionCall.Arguments)
			}
			m.Content = strings.TrimSpace(text)
			m.ToolCalls = nil
			m.FunctionCall = nil
		}

		normalized = append(normalized, m)
	}

	return normalized
}

func buildCompatResponse(upstreamResp *model.ChatCompletionResponse) *model.ChatCompletionResponse {
	resp := &model.ChatCompletionResponse{
		ID:      fallbackChatID(upstreamResp),
		Object:  fallbackChatObject(upstreamResp),
		Created: fallbackChatCreated(upstreamResp),
		Model:   fallbackChatModel(upstreamResp),
		Usage:   upstreamResp.Usage,
		Choices: []model.Choice{
			{
				Index: 0,
				Message: &model.Message{
					Role: "assistant",
				},
				FinishReason: "stop",
			},
		},
	}

	text := extractResponseText(upstreamResp)
	toolName, toolArgs, finalText, ok := parseCompatOutput(text)
	if ok && toolName != "" {
		resp.Choices[0].Message.Content = ""
		resp.Choices[0].Message.ToolCalls = []model.ToolCall{
			{
				ID:   fmt.Sprintf("call_%d", time.Now().UnixNano()),
				Type: "function",
				Function: model.FunctionCall{
					Name:      toolName,
					Arguments: toolArgs,
				},
			},
		}
		resp.Choices[0].FinishReason = "tool_calls"
		return resp
	}

	if finalText == "" {
		finalText = text
	}
	if finalText == "" {
		finalText = "(empty response)"
	}
	resp.Choices[0].Message.Content = finalText
	return resp
}

func parseCompatOutput(text string) (toolName string, toolArgs string, final string, ok bool) {
	clean := stripCodeFence(text)
	if clean == "" {
		return "", "", "", false
	}

	var payload fcCompatPayload
	if err := json.Unmarshal([]byte(clean), &payload); err != nil {
		return "", "", "", false
	}

	if payload.ToolCall != nil && strings.TrimSpace(payload.ToolCall.Name) != "" {
		args := strings.TrimSpace(string(payload.ToolCall.Arguments))
		if args == "" || args == "null" {
			args = "{}"
		}
		if !json.Valid([]byte(args)) {
			b, _ := json.Marshal(map[string]string{"input": args})
			args = string(b)
		}
		return strings.TrimSpace(payload.ToolCall.Name), args, "", true
	}

	if strings.TrimSpace(payload.Final) != "" {
		return "", "", strings.TrimSpace(payload.Final), true
	}

	return "", "", "", false
}

func stripCodeFence(text string) string {
	s := strings.TrimSpace(text)
	if !strings.HasPrefix(s, "```") {
		return s
	}

	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```JSON")
	s = strings.TrimPrefix(s, "```")
	if idx := strings.LastIndex(s, "```"); idx >= 0 {
		s = s[:idx]
	}
	return strings.TrimSpace(s)
}

func extractResponseText(resp *model.ChatCompletionResponse) string {
	if resp == nil || len(resp.Choices) == 0 || resp.Choices[0].Message == nil {
		return ""
	}
	return strings.TrimSpace(extractContentText(resp.Choices[0].Message.Content))
}

func extractContentText(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var sb strings.Builder
		for _, part := range v {
			if p, ok := part.(map[string]any); ok {
				if t, _ := p["type"].(string); t == "text" {
					if text, _ := p["text"].(string); text != "" {
						if sb.Len() > 0 {
							sb.WriteString("\n")
						}
						sb.WriteString(text)
					}
				}
			}
		}
		if sb.Len() > 0 {
			return sb.String()
		}
		b, _ := json.Marshal(v)
		return string(b)
	case map[string]any:
		if text, ok := v["text"].(string); ok {
			return text
		}
		b, _ := json.Marshal(v)
		return string(b)
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

func fallbackChatID(resp *model.ChatCompletionResponse) string {
	if resp != nil && strings.TrimSpace(resp.ID) != "" {
		return resp.ID
	}
	return fmt.Sprintf("chatcmpl-fusionapi-%d", time.Now().UnixNano())
}

func fallbackChatObject(resp *model.ChatCompletionResponse) string {
	if resp != nil && strings.TrimSpace(resp.Object) != "" {
		return resp.Object
	}
	return "chat.completion"
}

func fallbackChatCreated(resp *model.ChatCompletionResponse) int64 {
	if resp != nil && resp.Created > 0 {
		return resp.Created
	}
	return time.Now().Unix()
}

func fallbackChatModel(resp *model.ChatCompletionResponse) string {
	if resp != nil {
		return resp.Model
	}
	return ""
}

func writeCompatStreamResponse(c *gin.Context, resp *model.ChatCompletionResponse) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Transfer-Encoding", "chunked")

	if resp == nil || len(resp.Choices) == 0 || resp.Choices[0].Message == nil {
		fmt.Fprintf(c.Writer, "data: [DONE]\n\n")
		c.Writer.Flush()
		return
	}

	msg := resp.Choices[0].Message
	firstDelta := &model.Message{
		Role: "assistant",
	}
	if len(msg.ToolCalls) > 0 {
		firstDelta.ToolCalls = msg.ToolCalls
		firstDelta.Content = ""
	} else {
		firstDelta.Content = msg.Content
	}

	firstChunk := model.StreamChunk{
		ID:      resp.ID,
		Object:  "chat.completion.chunk",
		Created: time.Now().Unix(),
		Model:   resp.Model,
		Choices: []model.Choice{
			{
				Index: 0,
				Delta: firstDelta,
			},
		},
	}

	b, _ := json.Marshal(firstChunk)
	fmt.Fprintf(c.Writer, "data: %s\n\n", b)
	c.Writer.Flush()

	endChunk := model.StreamChunk{
		ID:      resp.ID,
		Object:  "chat.completion.chunk",
		Created: firstChunk.Created,
		Model:   resp.Model,
		Choices: []model.Choice{
			{
				Index:        0,
				FinishReason: resp.Choices[0].FinishReason,
			},
		},
	}
	endData, _ := json.Marshal(endChunk)
	fmt.Fprintf(c.Writer, "data: %s\n\n", endData)
	fmt.Fprintf(c.Writer, "data: [DONE]\n\n")
	c.Writer.Flush()
}
