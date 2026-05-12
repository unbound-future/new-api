package prom_metrics

import (
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/types"
)

func TestSanitizeLabel(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", "unknown"},
		{"  ", "unknown"},
		{"normal", "normal"},
		{strings.Repeat("a", 100), strings.Repeat("a", 64)},
	}
	for _, c := range cases {
		got := sanitizeLabel(c.in)
		if got != c.want {
			t.Errorf("sanitizeLabel(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestSanitizeMultibyteTruncate(t *testing.T) {
	// 65 个 "中" 字符,UTF-8 下每个 3 字节;按 rune 截断到 64 个
	in := strings.Repeat("中", 65)
	got := sanitizeLabel(in)
	if r := []rune(got); len(r) != 64 {
		t.Fatalf("expected 64 runes, got %d", len(r))
	}
}

func TestNormalizeAPIType(t *testing.T) {
	cases := []struct {
		format types.RelayFormat
		path   string
		want   string
	}{
		{types.RelayFormatOpenAI, "/v1/chat/completions", "chat"},
		{types.RelayFormatEmbedding, "/v1/embeddings", "embedding"},
		{types.RelayFormatOpenAIImage, "/v1/images/generations", "image"},
		{types.RelayFormatOpenAIAudio, "/v1/audio/transcriptions", "audio"},
		{types.RelayFormatRerank, "/v1/rerank", "rerank"},
		{types.RelayFormatClaude, "/v1/messages", "claude"},
		{types.RelayFormatGemini, "/v1beta/models/x:generateContent", "gemini"},
		{types.RelayFormatOpenAIRealtime, "/v1/realtime", "realtime"},
		{types.RelayFormatMjProxy, "/mj-proxy/submit/imagine", "mj"},
		{types.RelayFormatTask, "/suno/submit/music", "suno"},
		{"weird", "/something", "other"},
	}
	for _, c := range cases {
		got := NormalizeAPIType(c.format, c.path)
		if got != c.want {
			t.Errorf("NormalizeAPIType(%q,%q)=%q want %q", c.format, c.path, got, c.want)
		}
	}
}

func TestDeriveAPITypeFromPath(t *testing.T) {
	cases := []struct {
		path, want string
	}{
		{"/mj/submit/imagine", "mj"},
		{"/mj-fast/mj/submit/imagine", "mj"},
		{"/suno/submit/music", "suno"},
		{"/v1/rerank", "rerank"},
		{"/v1/audio/speech", "audio"},
		{"/v1/images/edits", "image"},
		{"/v1/embeddings", "embedding"},
		{"/v1/chat/completions", "chat"},
		{"/v1/realtime", "realtime"},
		{"/v1/messages", "claude"},
		{"/foo", "other"},
	}
	for _, c := range cases {
		got := deriveAPITypeFromPath(c.path)
		if got != c.want {
			t.Errorf("deriveAPITypeFromPath(%q)=%q want %q", c.path, got, c.want)
		}
	}
}

func TestClassifyOutcome(t *testing.T) {
	cases := []struct {
		code          int
		wantStatus    string
		wantErrorType string
	}{
		{http.StatusOK, "success", "none"},
		{http.StatusCreated, "success", "none"},
		{http.StatusUnauthorized, "error", "forbidden"},
		{http.StatusForbidden, "error", "forbidden"},
		{http.StatusTooManyRequests, "error", "rate_limit"},
		{http.StatusGatewayTimeout, "error", "timeout"},
		{http.StatusBadRequest, "error", "upstream_4xx"},
		{http.StatusInternalServerError, "error", "upstream_5xx"},
		{0, "error", "internal"},
		{999, "error", "internal"},
	}
	for _, c := range cases {
		s, e := ClassifyOutcome(c.code)
		if s != c.wantStatus || e != c.wantErrorType {
			t.Errorf("ClassifyOutcome(%d)=(%q,%q) want (%q,%q)", c.code, s, e, c.wantStatus, c.wantErrorType)
		}
	}
}

func TestCoerceErrorType(t *testing.T) {
	if coerceErrorType("rate_limit") != "rate_limit" {
		t.Fatal("known value should pass through")
	}
	if coerceErrorType("blahblah") != "internal" {
		t.Fatal("unknown should coerce to internal")
	}
	if coerceErrorType("") != "none" {
		t.Fatal("empty should become none")
	}
}

func TestCoerceAPIType(t *testing.T) {
	if coerceAPIType("chat") != "chat" {
		t.Fatal("known value should pass through")
	}
	if coerceAPIType("weird") != "other" {
		t.Fatal("unknown should coerce to other")
	}
}
