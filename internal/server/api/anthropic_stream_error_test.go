package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/transformer/anthropic"
)

func TestWriteAnthropicSSEStream_ContextLengthError(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	stream := &errorAfterStream{err: &llm.ResponseError{
		StatusCode: http.StatusBadRequest,
		Detail: llm.ErrorDetail{
			Type:    "invalid_request_error",
			Code:    "context_length_exceeded",
			Message: "Your input exceeds the context window of this model.",
		},
	}}

	WriteAnthropicSSEStream(c, stream)

	var errorData string
	lines := strings.Split(w.Body.String(), "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "event:error") && i+1 < len(lines) {
			errorData = strings.TrimPrefix(lines[i+1], "data:")
			break
		}
	}
	require.NotEmpty(t, errorData)

	var payload struct {
		Type  string `json:"type"`
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal([]byte(errorData), &payload))
	require.Equal(t, "error", payload.Type)
	require.Equal(t, "invalid_request_error", payload.Error.Type)
	require.Contains(t, payload.Error.Message, "exceeds the context window")
	require.NotContains(t, errorData, `"code"`)
}

func TestFormatAnthropicStreamError_GenericError(t *testing.T) {
	payload, ok := FormatAnthropicStreamError(t.Context(), errors.New("stream failed")).(anthropic.AnthropicError)
	require.True(t, ok)
	require.Equal(t, "error", payload.Type)
	require.Equal(t, "api_error", payload.Error.Type)
	require.Equal(t, "stream failed", payload.Error.Message)
}
