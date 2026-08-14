package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/fx"

	"github.com/looplj/axonhub/internal/contexts"
	entprivacy "github.com/looplj/axonhub/internal/ent/privacy"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/internal/server/orchestrator"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
	"github.com/looplj/axonhub/llm/transformer/anthropic"
)

type AnthropicHandlersParams struct {
	fx.In

	GatewayConfig               AnthropicGatewayConfig
	ChannelService              *biz.ChannelService
	ModelService                *biz.ModelService
	DefaultSelector             *orchestrator.DefaultSelector
	RequestService              *biz.RequestService
	SystemService               *biz.SystemService
	UsageLogService             *biz.UsageLogService
	PromptService               *biz.PromptService
	PromptProtectionRuleService *biz.PromptProtectionRuleService
	QuotaService                *biz.QuotaService
	HttpClient                  *httpclient.HttpClient
	LiveStreamRegistry          *biz.LiveStreamRegistry
	ChannelLimiterManager       *orchestrator.ChannelLimiterManager
	ProviderQuotaStatusProvider orchestrator.ProviderQuotaStatusProvider
	SSEKeepAliveConfig          SSEKeepAliveConfig
}

type AnthropicHandlers struct {
	ChannelService            *biz.ChannelService
	ModelService              *biz.ModelService
	SystemService             *biz.SystemService
	GatewayModelAliasExcludes map[string]struct{}
	ChatCompletionHandlers    *ChatCompletionHandlers
}

type AnthropicGatewayConfig struct {
	ModelAliasExcludes []string
}

func NewAnthropicHandlers(params AnthropicHandlersParams) *AnthropicHandlers {
	aliasExcludes := make(map[string]struct{}, len(params.GatewayConfig.ModelAliasExcludes))
	for _, modelID := range params.GatewayConfig.ModelAliasExcludes {
		modelID = strings.ToLower(strings.TrimSpace(modelID))
		if modelID != "" {
			aliasExcludes[modelID] = struct{}{}
		}
	}

	return &AnthropicHandlers{
		ChatCompletionHandlers: &ChatCompletionHandlers{
			ChatCompletionOrchestrator: orchestrator.NewChatCompletionOrchestrator(
				params.ChannelService,
				params.DefaultSelector,
				params.RequestService,
				params.HttpClient,
				anthropic.NewInboundTransformer(),
				params.SystemService,
				params.UsageLogService,
				params.PromptService,
				params.QuotaService,
				params.PromptProtectionRuleService,
				params.LiveStreamRegistry,
				params.ChannelLimiterManager,
				params.ProviderQuotaStatusProvider,
			),
			sseKeepAlive:       params.SSEKeepAliveConfig,
			sseHeartbeatFormat: sseHeartbeatAnthropic,
		},
		ChannelService:            params.ChannelService,
		ModelService:              params.ModelService,
		SystemService:             params.SystemService,
		GatewayModelAliasExcludes: aliasExcludes,
	}
}

func (handlers *AnthropicHandlers) CreateMessage(c *gin.Context) {
	handlers.ChatCompletionHandlers.WithStreamWriter(WriteAnthropicSSEStream).ChatCompletion(c)
}

// WriteAnthropicSSEStream preserves Anthropic's error event envelope for
// failures that arrive after an SSE response has started. Claude Code relies
// on this shape to recognize prompt-too-long errors and run its compaction
// recovery path.
func WriteAnthropicSSEStream(c *gin.Context, stream streams.Stream[*httpclient.StreamEvent]) {
	WriteSSEStreamWithErrorFormatter(c, stream, FormatAnthropicStreamError)
}

// FormatAnthropicStreamError formats a late stream failure using Anthropic's
// standard {"type":"error","error":{...}} envelope.
func FormatAnthropicStreamError(_ context.Context, err error) any {
	err = wrapQuotaExhaustedAsResponseError(err)

	errorType := "api_error"
	message := orchestrator.ExtractErrorMessage(err)
	requestID := ""

	var responseErr *llm.ResponseError
	if errors.As(err, &responseErr) {
		if responseErr.Detail.Type != "" {
			errorType = responseErr.Detail.Type
		}
		if responseErr.Detail.Message != "" {
			message = responseErr.Detail.Message
		}
		requestID = responseErr.Detail.RequestID
	}

	return anthropic.AnthropicError{
		Type:      "error",
		RequestID: requestID,
		Error: anthropic.ErrorDetail{
			Type:    errorType,
			Message: message,
		},
	}
}

type AnthropicModel struct {
	ID          string    `json:"id"`
	Type        string    `json:"type"`
	DisplayName string    `json:"display_name"`
	CreatedAt   time.Time `json:"created"`
}

// ListModels returns all available models.
// It uses QueryAllChannelModels setting from system config to determine model source.
func (handlers *AnthropicHandlers) ListModels(c *gin.Context) {
	ctx := c.Request.Context()

	models, err := handlers.ModelService.ListEnabledModels(ctx)
	if err != nil {
		requestID, _ := contexts.GetRequestID(ctx)
		status := http.StatusInternalServerError
		errorType := "internal_server_error"
		if errors.Is(err, entprivacy.Deny) {
			status = http.StatusForbidden
			errorType = "permission_error"
		}
		_ = c.Error(err)
		c.JSON(status, anthropic.AnthropicError{
			StatusCode: status,
			Type:       errorType,
			RequestID:  requestID,
			Error: anthropic.ErrorDetail{
				Type:    errorType,
				Message: err.Error(),
			},
		})

		return
	}

	anthropicModels := anthropicModelsWithGatewayAliases(models, handlers.GatewayModelAliasExcludes)

	var firstID string
	if len(anthropicModels) > 0 {
		firstID = anthropicModels[0].ID
	}

	var lastID string
	if len(anthropicModels) > 0 {
		lastID = anthropicModels[len(anthropicModels)-1].ID
	}

	c.JSON(http.StatusOK, gin.H{
		"object":   "list",
		"data":     anthropicModels,
		"has_more": false,
		"first_id": firstID,
		"last_id":  lastID,
	})
}

func anthropicModelsWithGatewayAliases(models []biz.ModelFacade, aliasExcludes map[string]struct{}) []AnthropicModel {
	result := make([]AnthropicModel, 0, len(models)*2)
	for _, model := range models {
		anthropicModel := AnthropicModel{
			ID:          model.ID,
			Type:        "model",
			DisplayName: model.DisplayName,
			CreatedAt:   model.CreatedAt,
		}
		result = append(result, anthropicModel)

		// Claude Code ignores discovered model IDs that do not start with
		// "claude" or "anthropic", including explicitly configured AxonHub
		// models. Emit aliases for both configured and channel-derived models.
		// Models already represented by Claude Code's fixed Opus/Sonnet/Haiku
		// slots can be excluded explicitly to avoid duplicate picker entries.
		_, excluded := aliasExcludes[strings.ToLower(strings.TrimSpace(model.ID))]
		if alias, ok := biz.ClaudeCodeGatewayModelAlias(model.ID); ok && !excluded {
			aliasModel := anthropicModel
			aliasModel.ID = alias
			result = append(result, aliasModel)
		}
	}

	return result
}
