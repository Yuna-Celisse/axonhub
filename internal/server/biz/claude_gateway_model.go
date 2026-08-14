package biz

import (
	"encoding/base64"
	"strings"
	"unicode/utf8"
)

const claudeCodeGatewayModelAliasPrefix = "claude-axonhub-"

// ClaudeCodeGatewayModelAlias returns a Claude-prefixed, reversible alias for
// a non-Anthropic model. Claude Code gateway discovery deliberately ignores
// IDs that do not start with "claude" or "anthropic".
func ClaudeCodeGatewayModelAlias(modelID string) (string, bool) {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return "", false
	}

	lowerModelID := strings.ToLower(modelID)
	if strings.HasPrefix(lowerModelID, "claude") || strings.HasPrefix(lowerModelID, "anthropic") {
		return "", false
	}

	encoded := base64.RawURLEncoding.EncodeToString([]byte(modelID))

	return claudeCodeGatewayModelAliasPrefix + encoded, true
}

// ResolveClaudeCodeGatewayModelAlias maps a discovery alias back to the model
// ID understood by AxonHub's normal channel selection and model mapping flow.
func ResolveClaudeCodeGatewayModelAlias(modelID string) string {
	if !strings.HasPrefix(modelID, claudeCodeGatewayModelAliasPrefix) {
		return modelID
	}

	encoded := strings.TrimPrefix(modelID, claudeCodeGatewayModelAliasPrefix)
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(decoded) == 0 || !utf8.Valid(decoded) {
		return modelID
	}

	return string(decoded)
}
