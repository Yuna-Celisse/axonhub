package biz

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/providerquotastatus"
	"github.com/looplj/axonhub/internal/ent/usagelog"
)

type UsageSummaryService struct {
	db            *ent.Client
	systemService *SystemService
}

func NewUsageSummaryService(db *ent.Client, systemService *SystemService) *UsageSummaryService {
	return &UsageSummaryService{db: db, systemService: systemService}
}

type UsageSummary struct {
	Period        string              `json:"period"`
	WindowStart   *time.Time          `json:"windowStart,omitempty"`
	APIKeyName    string              `json:"apiKeyName"`
	ActiveProfile string              `json:"activeProfile,omitempty"`
	Totals        UsageSummaryTotals  `json:"totals"`
	Models        []UsageSummaryModel `json:"models"`
	Subscriptions []UsageSubscription `json:"subscriptions"`
}

type UsageSummaryTotals struct {
	Requests                 int     `json:"requests"`
	InputTokens              int64   `json:"inputTokens"`
	CachedInputTokens        int64   `json:"cachedInputTokens"`
	CacheCreationInputTokens int64   `json:"cacheCreationInputTokens"`
	UncachedInputTokens      int64   `json:"uncachedInputTokens"`
	OutputTokens             int64   `json:"outputTokens"`
	ReasoningTokens          int64   `json:"reasoningTokens"`
	VisibleOutputTokens      int64   `json:"visibleOutputTokens"`
	TotalTokens              int64   `json:"totalTokens"`
	EquivalentCostUSD        float64 `json:"equivalentCostUSD"`
}

type UsageSummaryModel struct {
	ModelID                  string  `json:"modelID"`
	Requests                 int     `json:"requests"`
	InputTokens              int64   `json:"inputTokens"`
	CachedInputTokens        int64   `json:"cachedInputTokens"`
	CacheCreationInputTokens int64   `json:"cacheCreationInputTokens"`
	UncachedInputTokens      int64   `json:"uncachedInputTokens"`
	OutputTokens             int64   `json:"outputTokens"`
	ReasoningTokens          int64   `json:"reasoningTokens"`
	VisibleOutputTokens      int64   `json:"visibleOutputTokens"`
	TotalTokens              int64   `json:"totalTokens"`
	EquivalentCostUSD        float64 `json:"equivalentCostUSD"`
}

type UsageSubscription struct {
	ChannelID   int            `json:"channelID"`
	ChannelName string         `json:"channelName"`
	Provider    string         `json:"provider"`
	Status      string         `json:"status"`
	Ready       bool           `json:"ready"`
	NextResetAt *time.Time     `json:"nextResetAt,omitempty"`
	QuotaData   map[string]any `json:"quotaData"`
}

func (s *UsageSummaryService) Get(ctx context.Context, apiKey *ent.APIKey, period string) (*UsageSummary, error) {
	if apiKey == nil {
		return nil, fmt.Errorf("api key is required")
	}

	start, normalized, err := s.periodStart(ctx, period)
	if err != nil {
		return nil, err
	}

	return authz.RunWithSystemBypass(ctx, "usage-summary-current-api-key", func(queryCtx context.Context) (*UsageSummary, error) {
		base := s.db.UsageLog.Query().Where(usagelog.APIKeyID(apiKey.ID))
		if start != nil {
			base = base.Where(usagelog.CreatedAtGTE(*start))
		}

		type aggregateRow struct {
			Requests                 int      `json:"requests"`
			InputTokens              *int64   `json:"input_tokens"`
			CachedInputTokens        *int64   `json:"cached_input_tokens"`
			CacheCreationInputTokens *int64   `json:"cache_creation_input_tokens"`
			OutputTokens             *int64   `json:"output_tokens"`
			ReasoningTokens          *int64   `json:"reasoning_tokens"`
			TotalTokens              *int64   `json:"total_tokens"`
			Cost                     *float64 `json:"cost"`
		}

		var totals []aggregateRow
		err := base.Clone().Aggregate(
			ent.As(ent.Count(), "requests"),
			ent.As(ent.Sum(usagelog.FieldPromptTokens), "input_tokens"),
			ent.As(ent.Sum(usagelog.FieldPromptCachedTokens), "cached_input_tokens"),
			ent.As(ent.Sum(usagelog.FieldPromptWriteCachedTokens), "cache_creation_input_tokens"),
			ent.As(ent.Sum(usagelog.FieldCompletionTokens), "output_tokens"),
			ent.As(ent.Sum(usagelog.FieldCompletionReasoningTokens), "reasoning_tokens"),
			ent.As(ent.Sum(usagelog.FieldTotalTokens), "total_tokens"),
			ent.As(ent.Sum(usagelog.FieldTotalCost), "cost"),
		).Scan(queryCtx, &totals)
		if err != nil {
			return nil, fmt.Errorf("aggregate usage totals: %w", err)
		}

		type modelRow struct {
			ModelID                  string   `json:"model_id"`
			Requests                 int      `json:"requests"`
			InputTokens              int64    `json:"input_tokens"`
			CachedInputTokens        int64    `json:"cached_input_tokens"`
			CacheCreationInputTokens int64    `json:"cache_creation_input_tokens"`
			OutputTokens             int64    `json:"output_tokens"`
			ReasoningTokens          int64    `json:"reasoning_tokens"`
			TotalTokens              int64    `json:"total_tokens"`
			Cost                     *float64 `json:"cost"`
		}

		var modelRows []modelRow
		err = base.Clone().GroupBy(usagelog.FieldModelID).Aggregate(
			ent.As(ent.Count(), "requests"),
			ent.As(ent.Sum(usagelog.FieldPromptTokens), "input_tokens"),
			ent.As(ent.Sum(usagelog.FieldPromptCachedTokens), "cached_input_tokens"),
			ent.As(ent.Sum(usagelog.FieldPromptWriteCachedTokens), "cache_creation_input_tokens"),
			ent.As(ent.Sum(usagelog.FieldCompletionTokens), "output_tokens"),
			ent.As(ent.Sum(usagelog.FieldCompletionReasoningTokens), "reasoning_tokens"),
			ent.As(ent.Sum(usagelog.FieldTotalTokens), "total_tokens"),
			ent.As(ent.Sum(usagelog.FieldTotalCost), "cost"),
		).Scan(queryCtx, &modelRows)
		if err != nil {
			return nil, fmt.Errorf("aggregate usage by model: %w", err)
		}

		channelIDs, err := base.Clone().Select(usagelog.FieldChannelID).Ints(queryCtx)
		if err != nil {
			return nil, fmt.Errorf("list used channels: %w", err)
		}
		if profile := apiKey.GetActiveProfile(); profile != nil {
			channelIDs = append(channelIDs, profile.ChannelIDs...)
		}
		slices.Sort(channelIDs)
		channelIDs = slices.Compact(channelIDs)

		subscriptions := make([]UsageSubscription, 0)
		if len(channelIDs) > 0 {
			quotaRows, err := s.db.ProviderQuotaStatus.Query().
				Where(providerquotastatus.ChannelIDIn(channelIDs...)).All(queryCtx)
			if err != nil {
				return nil, fmt.Errorf("query provider quotas: %w", err)
			}
			channels, err := s.db.Channel.Query().Where(channel.IDIn(channelIDs...)).All(queryCtx)
			if err != nil {
				return nil, fmt.Errorf("query quota channels: %w", err)
			}
			channelNames := make(map[int]string, len(channels))
			for _, ch := range channels {
				channelNames[ch.ID] = ch.Name
			}
			for _, quota := range quotaRows {
				subscriptions = append(subscriptions, UsageSubscription{
					ChannelID: quota.ChannelID, ChannelName: channelNames[quota.ChannelID],
					Provider: string(quota.ProviderType), Status: string(quota.Status), Ready: quota.Ready,
					NextResetAt: quota.NextResetAt, QuotaData: quota.QuotaData,
				})
			}
		}

		result := &UsageSummary{Period: normalized, WindowStart: start, APIKeyName: apiKey.Name, Models: make([]UsageSummaryModel, 0, len(modelRows)), Subscriptions: subscriptions}
		if apiKey.Profiles != nil {
			result.ActiveProfile = apiKey.Profiles.ActiveProfile
		}
		if len(totals) > 0 {
			row := totals[0]
			inputTokens := int64Value(row.InputTokens)
			cachedInputTokens := int64Value(row.CachedInputTokens)
			cacheCreationInputTokens := int64Value(row.CacheCreationInputTokens)
			outputTokens := int64Value(row.OutputTokens)
			reasoningTokens := int64Value(row.ReasoningTokens)
			result.Totals = UsageSummaryTotals{
				Requests: row.Requests, InputTokens: inputTokens,
				CachedInputTokens: cachedInputTokens, CacheCreationInputTokens: cacheCreationInputTokens,
				UncachedInputTokens: nonNegativeDifference(inputTokens, cachedInputTokens, cacheCreationInputTokens),
				OutputTokens:        outputTokens, ReasoningTokens: reasoningTokens,
				VisibleOutputTokens: nonNegativeDifference(outputTokens, reasoningTokens), TotalTokens: int64Value(row.TotalTokens),
			}
			if row.Cost != nil {
				result.Totals.EquivalentCostUSD = *row.Cost
			}
		}
		for _, row := range modelRows {
			item := UsageSummaryModel{
				ModelID: row.ModelID, Requests: row.Requests, InputTokens: row.InputTokens,
				CachedInputTokens: row.CachedInputTokens, CacheCreationInputTokens: row.CacheCreationInputTokens,
				UncachedInputTokens: nonNegativeDifference(row.InputTokens, row.CachedInputTokens, row.CacheCreationInputTokens),
				OutputTokens:        row.OutputTokens, ReasoningTokens: row.ReasoningTokens,
				VisibleOutputTokens: nonNegativeDifference(row.OutputTokens, row.ReasoningTokens), TotalTokens: row.TotalTokens,
			}
			if row.Cost != nil {
				item.EquivalentCostUSD = *row.Cost
			}
			result.Models = append(result.Models, item)
		}
		slices.SortFunc(result.Models, func(a, b UsageSummaryModel) int { return cmp.Compare(b.TotalTokens, a.TotalTokens) })
		return result, nil
	})
}

func int64Value(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}

func nonNegativeDifference(total int64, parts ...int64) int64 {
	for _, part := range parts {
		total -= part
	}
	if total < 0 {
		return 0
	}
	return total
}

func (s *UsageSummaryService) periodStart(ctx context.Context, period string) (*time.Time, string, error) {
	now := time.Now().In(s.systemService.TimeLocation(ctx))
	switch period {
	case "", "month":
		start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).UTC()
		return &start, "month", nil
	case "today":
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).UTC()
		return &start, "today", nil
	case "7d":
		start := now.Add(-7 * 24 * time.Hour).UTC()
		return &start, "7d", nil
	case "all":
		return nil, "all", nil
	default:
		return nil, "", fmt.Errorf("unsupported period %q; use today, 7d, month, or all", period)
	}
}
