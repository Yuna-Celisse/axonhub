package biz

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/apikey"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/ent/providerquotastatus"
	"github.com/looplj/axonhub/internal/ent/request"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/pkg/xcache"
)

func TestUsageSummaryServiceCurrentAPIKey(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	t.Cleanup(func() { _ = client.Close() })

	ctx := ent.NewContext(authz.WithTestBypass(t.Context()), client)
	key, err := client.APIKey.Create().
		SetProjectID(1).
		SetKey("ah-test").
		SetName("Claude Code").
		SetType(apikey.TypeUser).
		SetStatus(apikey.StatusEnabled).
		Save(ctx)
	require.NoError(t, err)

	ch, err := client.Channel.Create().
		SetType(channel.TypeCodex).
		SetName("ChatGPT Plus").
		SetStatus(channel.StatusEnabled).
		SetSupportedModels([]string{"gpt-test"}).
		SetDefaultTestModel("gpt-test").
		SetCredentials(objects.ChannelCredentials{}).
		Save(ctx)
	require.NoError(t, err)

	req, err := client.Request.Create().
		SetProjectID(1).
		SetAPIKeyID(key.ID).
		SetModelID("gpt-test").
		SetRequestBody(objects.JSONRawMessage(`{}`)).
		SetStatus(request.StatusCompleted).
		SetChannelID(ch.ID).
		Save(ctx)
	require.NoError(t, err)

	cost := 1.25
	_, err = client.UsageLog.Create().
		SetRequestID(req.ID).
		SetAPIKeyID(key.ID).
		SetProjectID(1).
		SetChannelID(ch.ID).
		SetModelID("gpt-test").
		SetPromptTokens(100).
		SetPromptCachedTokens(40).
		SetPromptWriteCachedTokens(10).
		SetCompletionTokens(20).
		SetCompletionReasoningTokens(5).
		SetTotalTokens(120).
		SetTotalCost(cost).
		Save(ctx)
	require.NoError(t, err)

	reset := time.Now().Add(time.Hour).UTC()
	_, err = client.ProviderQuotaStatus.Create().
		SetChannelID(ch.ID).
		SetProviderType(providerquotastatus.ProviderTypeCodex).
		SetStatus(providerquotastatus.StatusAvailable).
		SetReady(true).
		SetQuotaData(map[string]any{"plan_type": "plus"}).
		SetNextResetAt(reset).
		SetNextCheckAt(reset).
		Save(ctx)
	require.NoError(t, err)

	systemService := NewSystemService(SystemServiceParams{Ent: client, CacheConfig: xcache.Config{Mode: xcache.ModeMemory}})
	summary, err := NewUsageSummaryService(client, systemService).Get(ctx, key, "all")
	require.NoError(t, err)
	require.Equal(t, 1, summary.Totals.Requests)
	require.Equal(t, int64(120), summary.Totals.TotalTokens)
	require.Equal(t, int64(100), summary.Totals.InputTokens)
	require.Equal(t, int64(40), summary.Totals.CachedInputTokens)
	require.Equal(t, int64(10), summary.Totals.CacheCreationInputTokens)
	require.Equal(t, int64(50), summary.Totals.UncachedInputTokens)
	require.Equal(t, int64(20), summary.Totals.OutputTokens)
	require.Equal(t, int64(5), summary.Totals.ReasoningTokens)
	require.Equal(t, int64(15), summary.Totals.VisibleOutputTokens)
	require.Equal(t, cost, summary.Totals.EquivalentCostUSD)
	require.Len(t, summary.Models, 1)
	require.Equal(t, "gpt-test", summary.Models[0].ModelID)
	require.Equal(t, int64(10), summary.Models[0].CacheCreationInputTokens)
	require.Equal(t, int64(50), summary.Models[0].UncachedInputTokens)
	require.Equal(t, int64(5), summary.Models[0].ReasoningTokens)
	require.Equal(t, int64(15), summary.Models[0].VisibleOutputTokens)
	require.Len(t, summary.Subscriptions, 1)
	require.Equal(t, "ChatGPT Plus", summary.Subscriptions[0].ChannelName)
	require.Equal(t, "plus", summary.Subscriptions[0].QuotaData["plan_type"])
}

func TestUsageSummaryServiceEmptyUsage(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:empty?mode=memory&_fk=0")
	t.Cleanup(func() { _ = client.Close() })
	ctx := ent.NewContext(authz.WithTestBypass(t.Context()), client)
	key, err := client.APIKey.Create().SetProjectID(1).SetKey("ah-empty").SetName("Empty").SetType(apikey.TypeUser).SetStatus(apikey.StatusEnabled).Save(ctx)
	require.NoError(t, err)
	systemService := NewSystemService(SystemServiceParams{Ent: client, CacheConfig: xcache.Config{Mode: xcache.ModeMemory}})
	summary, err := NewUsageSummaryService(client, systemService).Get(ctx, key, "all")
	require.NoError(t, err)
	require.Zero(t, summary.Totals.TotalTokens)
	require.Empty(t, summary.Models)
}
