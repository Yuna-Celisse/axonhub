package biz

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClaudeCodeGatewayModelAliasRoundTrip(t *testing.T) {
	for _, modelID := range []string{"glm-5.2", "kimi-k3", "provider/model.v2"} {
		alias, ok := ClaudeCodeGatewayModelAlias(modelID)
		require.True(t, ok)
		require.Contains(t, alias, claudeCodeGatewayModelAliasPrefix)
		require.Equal(t, modelID, ResolveClaudeCodeGatewayModelAlias(alias))
	}
}

func TestClaudeCodeGatewayModelAliasLeavesAnthropicIDsAlone(t *testing.T) {
	for _, modelID := range []string{"claude-sonnet-4-6", "anthropic.claude-opus"} {
		alias, ok := ClaudeCodeGatewayModelAlias(modelID)
		require.False(t, ok)
		require.Empty(t, alias)
		require.Equal(t, modelID, ResolveClaudeCodeGatewayModelAlias(modelID))
	}
}

func TestResolveClaudeCodeGatewayModelAliasRejectsMalformedAlias(t *testing.T) {
	modelID := claudeCodeGatewayModelAliasPrefix + "%%%"
	require.Equal(t, modelID, ResolveClaudeCodeGatewayModelAlias(modelID))
}
