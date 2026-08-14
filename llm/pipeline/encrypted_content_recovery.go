package pipeline

import (
	"errors"
	"net/http"
	"strings"

	"github.com/looplj/axonhub/llm"
)

const invalidEncryptedContentCode = "invalid_encrypted_content"

func isInvalidEncryptedContentError(err error) bool {
	if !IsUpstreamError(err) {
		return false
	}

	var responseErr *llm.ResponseError
	if !errors.As(err, &responseErr) {
		return false
	}

	if responseErr.StatusCode != http.StatusBadRequest && responseErr.StatusCode != http.StatusUnprocessableEntity {
		return false
	}

	if strings.EqualFold(responseErr.Detail.Code, invalidEncryptedContentCode) {
		return true
	}

	message := strings.ToLower(strings.TrimSpace(responseErr.Detail.Message))
	mentionsPrivateReasoning := strings.Contains(message, "encrypted content") ||
		strings.Contains(message, "reasoning signature") ||
		strings.Contains(message, "thinking signature")
	rejectsPrivateReasoning := strings.Contains(message, "invalid") ||
		strings.Contains(message, "could not be verified") ||
		strings.Contains(message, "could not be decrypted") ||
		strings.Contains(message, "could not be parsed") ||
		strings.Contains(message, "verification failed") ||
		strings.Contains(message, "signature mismatch")

	return mentionsPrivateReasoning && rejectsPrivateReasoning
}

func clearReasoningSignatures(request *llm.Request) bool {
	if request == nil {
		return false
	}

	cleared := clearMessageReasoningSignatures(request.Messages)
	if request.Compact != nil {
		cleared = clearMessageReasoningSignatures(request.Compact.Input) || cleared
	}

	return cleared
}

func clearMessageReasoningSignatures(messages []llm.Message) bool {
	cleared := false
	for i := range messages {
		if messages[i].ReasoningSignature == nil {
			continue
		}

		messages[i].ReasoningSignature = nil
		cleared = true
	}

	return cleared
}
