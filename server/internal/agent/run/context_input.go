package run

import (
	"fmt"

	agentcontext "github.com/1024XEngineer/XE3-ESL/server/internal/agent/context"
)

func textRequestFromContext(input agentcontext.ModelInput) (TextRequest, error) {
	request := TextRequest{Messages: make([]TextMessage, 0, len(input.Messages))}
	for index, message := range input.Messages {
		role, ok := textRoleFromContext(message.Role)
		if !ok {
			return TextRequest{}, fmt.Errorf("agent run: context message %d has an unsupported role", index)
		}
		mapped := TextMessage{Role: role, Content: message.Content}
		if len(message.ContentParts) > 0 {
			mapped.ContentParts = make([]ContentPart, 0, len(message.ContentParts))
			for partIndex, part := range message.ContentParts {
				kind, valid := contentPartKindFromContext(part.Kind)
				if !valid {
					return TextRequest{}, fmt.Errorf("agent run: context message %d part %d has an unsupported kind", index, partIndex)
				}
				mapped.ContentParts = append(mapped.ContentParts, ContentPart{
					Kind: kind, Text: part.Text, ImageURL: part.ImageURL,
				})
			}
		}
		request.Messages = append(request.Messages, mapped)
	}
	if err := ValidateTextRequest(request); err != nil {
		return TextRequest{}, fmt.Errorf("agent run: invalid context model input: %w", err)
	}
	return request, nil
}

func textRoleFromContext(role agentcontext.ModelRole) (TextRole, bool) {
	switch role {
	case agentcontext.ModelRoleSystem:
		return TextRoleSystem, true
	case agentcontext.ModelRoleUser:
		return TextRoleUser, true
	case agentcontext.ModelRoleAssistant:
		return TextRoleAssistant, true
	default:
		return "", false
	}
}

func contentPartKindFromContext(
	kind agentcontext.ModelContentPartKind,
) (ContentPartKind, bool) {
	switch kind {
	case agentcontext.ModelContentPartText:
		return ContentPartText, true
	case agentcontext.ModelContentPartImageURL:
		return ContentPartImageURL, true
	default:
		return "", false
	}
}
