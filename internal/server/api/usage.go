package api

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/fx"

	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/server/biz"
)

type UsageHandlersParams struct {
	fx.In

	UsageSummaryService *biz.UsageSummaryService
}

type UsageHandlers struct{ UsageSummaryService *biz.UsageSummaryService }

func NewUsageHandlers(params UsageHandlersParams) *UsageHandlers {
	return &UsageHandlers{UsageSummaryService: params.UsageSummaryService}
}

func (h *UsageHandlers) Summary(c *gin.Context) {
	apiKey, ok := contexts.GetAPIKey(c.Request.Context())
	if !ok || apiKey == nil {
		JSONError(c, http.StatusUnauthorized, errors.New("API key not found in context"))
		return
	}

	summary, err := h.UsageSummaryService.Get(c.Request.Context(), apiKey, c.Query("period"))
	if err != nil {
		JSONError(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, summary)
}
