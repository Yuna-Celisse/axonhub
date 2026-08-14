package shared

import (
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

func TestFormatPortableReasoningSummary(t *testing.T) {
	require.Nil(t, FormatPortableReasoningSummary(nil))
	require.Nil(t, FormatPortableReasoningSummary(lo.ToPtr("  ")))
	require.Equal(t,
		PortableReasoningSummaryPrefix+"Keep the completed analysis and continue with the patch.",
		lo.FromPtr(FormatPortableReasoningSummary(lo.ToPtr("Keep the completed analysis and continue with the patch."))),
	)
}
