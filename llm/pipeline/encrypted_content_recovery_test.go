package pipeline

import (
	"context"
	"net/http"
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
)

func TestPipelineRecoversFromInvalidEncryptedContent(t *testing.T) {
	request := &llm.Request{
		Stream: lo.ToPtr(true),
		Messages: []llm.Message{
			{Role: "user"},
			{Role: "assistant", ReasoningSignature: lo.ToPtr("gAAAA-stale")},
		},
	}

	inbound := &mockInbound{
		transformRequest: func(context.Context, *httpclient.Request) (*llm.Request, error) {
			return request, nil
		},
	}

	transformCalls := 0
	outbound := &mockOutbound{
		transformRequest: func(_ context.Context, request *llm.Request) (*httpclient.Request, error) {
			transformCalls++
			if transformCalls == 1 {
				require.NotNil(t, request.Messages[1].ReasoningSignature)
			} else {
				require.Nil(t, request.Messages[1].ReasoningSignature)
			}

			return &httpclient.Request{}, nil
		},
		transformError: func(context.Context, *httpclient.Error) *llm.ResponseError {
			return &llm.ResponseError{
				StatusCode: http.StatusBadRequest,
				Detail: llm.ErrorDetail{
					Code: "invalid_encrypted_content",
				},
			}
		},
		transformStream: func(_ context.Context, _ *httpclient.Request, _ streams.Stream[*httpclient.StreamEvent]) (streams.Stream[*llm.Response], error) {
			return streams.SliceStream([]*llm.Response{llm.DoneResponse}), nil
		},
	}

	executorCalls := 0
	executor := &mockExecutor{
		doStream: func(context.Context, *httpclient.Request) (streams.Stream[*httpclient.StreamEvent], error) {
			executorCalls++
			if executorCalls == 1 {
				return nil, &httpclient.Error{
					StatusCode: http.StatusBadRequest,
					Status:     http.StatusText(http.StatusBadRequest),
				}
			}

			return streams.SliceStream([]*httpclient.StreamEvent{}), nil
		},
	}

	p := &pipeline{
		Executor: executor,
		Inbound:  inbound,
		Outbound: outbound,
	}

	result, err := p.Process(t.Context(), &httpclient.Request{})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.Stream)
	require.Equal(t, 2, executorCalls)
	require.Equal(t, 2, transformCalls)
}

func TestPipelineDoesNotRecoverUnrelatedBadRequest(t *testing.T) {
	request := &llm.Request{
		Messages: []llm.Message{{
			Role:               "assistant",
			ReasoningSignature: lo.ToPtr("gAAAA-valid"),
		}},
	}

	inbound := &mockInbound{
		transformRequest: func(context.Context, *httpclient.Request) (*llm.Request, error) {
			return request, nil
		},
	}

	outbound := &mockOutbound{
		transformError: func(context.Context, *httpclient.Error) *llm.ResponseError {
			return &llm.ResponseError{
				StatusCode: http.StatusBadRequest,
				Detail: llm.ErrorDetail{
					Code: "invalid_request_error",
				},
			}
		},
	}

	executorCalls := 0
	executor := &mockExecutor{
		do: func(context.Context, *httpclient.Request) (*httpclient.Response, error) {
			executorCalls++
			return nil, &httpclient.Error{
				StatusCode: http.StatusBadRequest,
				Status:     http.StatusText(http.StatusBadRequest),
			}
		},
	}

	p := &pipeline{
		Executor: executor,
		Inbound:  inbound,
		Outbound: outbound,
	}

	result, err := p.Process(t.Context(), &httpclient.Request{})
	require.Error(t, err)
	require.Nil(t, result)
	require.Equal(t, 1, executorCalls)
	require.NotNil(t, request.Messages[0].ReasoningSignature)
}

func TestClearReasoningSignaturesIncludesCompactInput(t *testing.T) {
	request := &llm.Request{
		Messages: []llm.Message{{ReasoningSignature: lo.ToPtr("message-signature")}},
		Compact: &llm.CompactRequest{
			Input: []llm.Message{{ReasoningSignature: lo.ToPtr("compact-signature")}},
		},
	}

	require.True(t, clearReasoningSignatures(request))
	require.Nil(t, request.Messages[0].ReasoningSignature)
	require.Nil(t, request.Compact.Input[0].ReasoningSignature)
	require.False(t, clearReasoningSignatures(request))
}

func TestIsInvalidEncryptedContentErrorRecognizesProviderVariants(t *testing.T) {
	tests := []struct {
		name   string
		status int
		detail llm.ErrorDetail
		want   bool
	}{
		{
			name:   "OpenAI error code",
			status: http.StatusBadRequest,
			detail: llm.ErrorDetail{Code: invalidEncryptedContentCode},
			want:   true,
		},
		{
			name:   "Codex encrypted content message",
			status: http.StatusBadRequest,
			detail: llm.ErrorDetail{Message: "The encrypted content could not be verified. Reason: Encrypted content could not be decrypted or parsed."},
			want:   true,
		},
		{
			name:   "Anthropic thinking signature message",
			status: http.StatusUnprocessableEntity,
			detail: llm.ErrorDetail{Message: "Invalid thinking signature in assistant content"},
			want:   true,
		},
		{
			name:   "unrelated invalid request",
			status: http.StatusBadRequest,
			detail: llm.ErrorDetail{Message: "Invalid tool schema"},
			want:   false,
		},
		{
			name:   "signature error with non-client status",
			status: http.StatusInternalServerError,
			detail: llm.ErrorDetail{Message: "Invalid reasoning signature"},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := WrapUpstreamError(&llm.ResponseError{StatusCode: tt.status, Detail: tt.detail})
			require.Equal(t, tt.want, isInvalidEncryptedContentError(err))
		})
	}
}
