package prom_metrics

import (
	"strings"

	"github.com/QuantumNous/new-api/types"
)

const (
	LabelUnknown  = "unknown"
	MaxLabelRunes = 64
)

// API type 枚举。越界归 "other"。
var validAPITypes = map[string]struct{}{
	"chat":      {},
	"embedding": {},
	"image":     {},
	"audio":     {},
	"rerank":    {},
	"claude":    {},
	"gemini":    {},
	"mj":        {},
	"suno":      {},
	"realtime":  {},
	"other":     {},
}

// error_type 枚举。"none" 代表无错误;越界归 "internal";空串视为 "none"。
var validErrorTypes = map[string]struct{}{
	"none":             {},
	"upstream_4xx":     {},
	"upstream_5xx":     {},
	"quota_not_enough": {},
	"rate_limit":       {},
	"timeout":          {},
	"forbidden":        {},
	"internal":         {},
	"canceled":         {},
}

// sanitizeLabel 空白替换为 "unknown",按 rune 截断到 MaxLabelRunes 字符。
func sanitizeLabel(in string) string {
	s := strings.TrimSpace(in)
	if s == "" {
		return LabelUnknown
	}
	r := []rune(s)
	if len(r) > MaxLabelRunes {
		return string(r[:MaxLabelRunes])
	}
	return s
}

func coerceAPIType(in string) string {
	if _, ok := validAPITypes[in]; ok {
		return in
	}
	return "other"
}

func coerceErrorType(in string) string {
	if in == "" {
		return "none"
	}
	if _, ok := validErrorTypes[in]; ok {
		return in
	}
	return "internal"
}

// NormalizeAPIType 优先按 RelayFormat 归类;未识别时回退到 path 启发式。
func NormalizeAPIType(format types.RelayFormat, path string) string {
	switch format {
	case types.RelayFormatOpenAI:
		if v := deriveAPITypeFromPath(path); v != "other" {
			return v
		}
		return "chat"
	case types.RelayFormatEmbedding:
		return "embedding"
	case types.RelayFormatOpenAIImage:
		return "image"
	case types.RelayFormatOpenAIAudio:
		return "audio"
	case types.RelayFormatRerank:
		return "rerank"
	case types.RelayFormatClaude:
		return "claude"
	case types.RelayFormatGemini:
		return "gemini"
	case types.RelayFormatOpenAIRealtime:
		return "realtime"
	case types.RelayFormatOpenAIResponses, types.RelayFormatOpenAIResponsesCompaction:
		return "chat"
	}
	return deriveAPITypeFromPath(path)
}

// deriveAPITypeFromPath 按 URL 前缀启发式判定。仅作 fallback。
func deriveAPITypeFromPath(path string) string {
	switch {
	case strings.Contains(path, "/mj/"):
		return "mj"
	case strings.HasPrefix(path, "/suno/"):
		return "suno"
	case strings.HasSuffix(path, "/realtime"):
		return "realtime"
	case strings.HasSuffix(path, "/messages"):
		return "claude"
	case strings.Contains(path, "/embeddings"):
		return "embedding"
	case strings.Contains(path, "/audio/"):
		return "audio"
	case strings.Contains(path, "/images/"):
		return "image"
	case strings.HasSuffix(path, "/rerank"):
		return "rerank"
	case strings.Contains(path, "/chat/") || strings.HasSuffix(path, "/completions") || strings.HasSuffix(path, "/responses"):
		return "chat"
	case strings.Contains(path, "/v1beta/"):
		return "gemini"
	}
	return "other"
}

// ClassifyOutcome 根据 HTTP status code 推导 (status, error_type)。
// 不做任何错误文案匹配。后续若需细化业务错误,由中间件从专用 ContextKey 覆盖。
func ClassifyOutcome(statusCode int) (status string, errorType string) {
	switch {
	case statusCode >= 200 && statusCode < 300:
		return "success", "none"
	case statusCode == 401 || statusCode == 403:
		return "error", "forbidden"
	case statusCode == 429:
		return "error", "rate_limit"
	case statusCode == 504:
		return "error", "timeout"
	case statusCode >= 400 && statusCode < 500:
		return "error", "upstream_4xx"
	case statusCode >= 500 && statusCode < 600:
		return "error", "upstream_5xx"
	default:
		return "error", "internal"
	}
}
