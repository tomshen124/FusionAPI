package core

import (
	"github.com/xiaopang/fusionapi/internal/model"
)

// Translator 请求/响应转换器
type Translator struct{}

// NewTranslator 创建转换器
func NewTranslator() *Translator {
	return &Translator{}
}

// TranslateRequest 转换请求
func (t *Translator) TranslateRequest(req *model.ChatCompletionRequest, src *model.Source) *model.ChatCompletionRequest {
	// 复制请求，避免修改原始数据
	translated := *req

	// CPA 特殊处理
	if src.Type == model.SourceTypeCPA {
		// CPA 不支持 Thinking，强制移除
		translated.Thinking = nil

		// FC 按 provider 判断
		if translated.HasTools() && !src.SupportsFCForModel(req.Model) {
			translated = t.degradeFCToPrompt(&translated)
		}
		return &translated
	}

	// FC 处理
	if translated.HasTools() && !src.Capabilities.FunctionCalling {
		translated = t.degradeFCToPrompt(&translated)
	}

	// Thinking 处理
	if translated.HasThinking() && !src.Capabilities.ExtendedThinking {
		translated.Thinking = nil
	}

	// 根据源类型做特定转换
	switch src.Type {
	case model.SourceTypeAnthropic:
		return t.toAnthropicFormat(&translated)
	default:
		return &translated
	}
}

// degradeFCToPrompt FC 降级为 prompt
func (t *Translator) degradeFCToPrompt(req *model.ChatCompletionRequest) model.ChatCompletionRequest {
	// 简单降级：将工具定义添加到 system prompt
	degraded := *req
	degraded.Tools = nil
	degraded.Functions = nil
	degraded.ToolChoice = nil
	degraded.FunctionCall = nil

	// TODO: 可以将工具定义序列化后添加到 system message
	// 目前先简单清除

	return degraded
}

// toAnthropicFormat 转换为 Anthropic 格式
func (t *Translator) toAnthropicFormat(req *model.ChatCompletionRequest) *model.ChatCompletionRequest {
	// OpenAI 和 Anthropic 的消息格式兼容性处理
	// 大部分情况下，使用 OpenAI 兼容端点的 Anthropic 服务（如通过反代）不需要特殊处理
	// 这里预留扩展点

	return req
}

// TranslateResponse 转换响应
func (t *Translator) TranslateResponse(resp *model.ChatCompletionResponse, src *model.Source) *model.ChatCompletionResponse {
	// 目前直接返回，预留扩展点
	return resp
}

// TranslateStreamChunk 转换流式响应块
func (t *Translator) TranslateStreamChunk(chunk *model.StreamChunk, src *model.Source) *model.StreamChunk {
	// 目前直接返回，预留扩展点
	return chunk
}

// TranslateError 转换错误响应
func (t *Translator) TranslateError(err error, src *model.Source) *model.ErrorResponse {
	return &model.ErrorResponse{
		Error: model.ErrorDetail{
			Message: err.Error(),
			Type:    "upstream_error",
			Code:    "source_error",
		},
	}
}
