package shared

import "strings"

const PortableReasoningSummaryPrefix = "[Reasoning summary from a previous model]\n"

// FormatPortableReasoningSummary turns a provider reasoning summary into normal
// message context when its private signature cannot be reused by the target.
func FormatPortableReasoningSummary(content *string) *string {
	if content == nil || strings.TrimSpace(*content) == "" {
		return nil
	}

	formatted := PortableReasoningSummaryPrefix + *content

	return &formatted
}
