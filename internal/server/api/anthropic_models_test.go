package api

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/server/biz"
)

func TestAnthropicModelsWithGatewayAliases(t *testing.T) {
	createdAt := time.Unix(123, 0)
	models := anthropicModelsWithGatewayAliases([]biz.ModelFacade{
		{ID: "glm-5.2", DisplayName: "GLM 5.2", CreatedAt: createdAt, OwnedBy: "opencode_go"},
		{ID: "gpt-5.6-sol", DisplayName: "GPT 5.6 Sol", CreatedAt: createdAt, OwnedBy: "configured"},
		{ID: "claude-sonnet-4-6", DisplayName: "Claude Sonnet 4.6", CreatedAt: createdAt},
	}, nil)

	require.Len(t, models, 5)
	require.Equal(t, "glm-5.2", models[0].ID)
	require.Equal(t, "GLM 5.2", models[0].DisplayName)
	require.Equal(t, "glm-5.2", biz.ResolveClaudeCodeGatewayModelAlias(models[1].ID))
	require.Equal(t, "GLM 5.2", models[1].DisplayName)
	require.Equal(t, "gpt-5.6-sol", models[2].ID)
	require.Equal(t, "gpt-5.6-sol", biz.ResolveClaudeCodeGatewayModelAlias(models[3].ID))
	require.Equal(t, "claude-sonnet-4-6", models[4].ID)
}

func TestAnthropicModelsWithGatewayAliasesExcludesFixedSlotModels(t *testing.T) {
	createdAt := time.Unix(123, 0)
	models := anthropicModelsWithGatewayAliases([]biz.ModelFacade{
		{ID: "deepseek-v4-flash", DisplayName: "DeepSeek V4 Flash", CreatedAt: createdAt, OwnedBy: "configured"},
		{ID: "kimi-k3", DisplayName: "Kimi K3", CreatedAt: createdAt, OwnedBy: "configured"},
	}, map[string]struct{}{
		"deepseek-v4-flash": {},
	})

	require.Len(t, models, 3)
	require.Equal(t, "deepseek-v4-flash", models[0].ID)
	require.Equal(t, "kimi-k3", models[1].ID)
	require.Equal(t, "kimi-k3", biz.ResolveClaudeCodeGatewayModelAlias(models[2].ID))
}
