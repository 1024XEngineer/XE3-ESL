package context

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

const (
	maxMessageContentBytes = 16 * 1024
	maxImageContentParts   = 4
	maxImageURLBytes       = 16 * 1024
)

// ModelInput is the trusted, budgeted input assembled for one Agent run.
// Tool routing and provider transport metadata are added by the Run boundary.
type ModelInput struct {
	Messages []ModelMessage
}

type ModelRole string

const (
	ModelRoleSystem    ModelRole = "system"
	ModelRoleUser      ModelRole = "user"
	ModelRoleAssistant ModelRole = "assistant"
)

type ModelContentPartKind string

const (
	ModelContentPartText     ModelContentPartKind = "text"
	ModelContentPartImageURL ModelContentPartKind = "image_url"
)

// ModelContentPart contains only the multimodal data selected for this run.
// ImageURL is an ephemeral HTTPS capability and must not be persisted.
type ModelContentPart struct {
	Kind     ModelContentPartKind
	Text     string
	ImageURL string
}

type ModelMessage struct {
	Role         ModelRole
	Content      string
	ContentParts []ModelContentPart
}

func validateModelInput(input ModelInput) error {
	if len(input.Messages) < 2 {
		return errors.New("agent context model input requires system and user messages")
	}
	for index, message := range input.Messages {
		switch message.Role {
		case ModelRoleSystem:
			if strings.TrimSpace(message.Content) == "" ||
				len(message.ContentParts) != 0 {
				return fmt.Errorf("agent context model message %d has invalid system content", index)
			}
		case ModelRoleUser:
			if err := validateModelUserContent(message); err != nil {
				return fmt.Errorf("agent context model message %d has invalid user content: %w", index, err)
			}
		case ModelRoleAssistant:
			if strings.TrimSpace(message.Content) == "" ||
				len(message.ContentParts) != 0 {
				return fmt.Errorf("agent context model message %d has invalid assistant content", index)
			}
		default:
			return fmt.Errorf("agent context model message %d has an unsupported role", index)
		}
	}
	if input.Messages[0].Role != ModelRoleSystem ||
		input.Messages[len(input.Messages)-1].Role != ModelRoleUser {
		return errors.New("agent context model input has an invalid message order")
	}
	return nil
}

func validateModelUserContent(message ModelMessage) error {
	hasText := strings.TrimSpace(message.Content) != ""
	if len(message.ContentParts) == 0 {
		if !hasText {
			return errors.New("content is empty")
		}
		return nil
	}
	if hasText {
		return errors.New("content and content parts are mutually exclusive")
	}

	textParts := 0
	imageParts := 0
	for _, part := range message.ContentParts {
		switch part.Kind {
		case ModelContentPartText:
			if strings.TrimSpace(part.Text) == "" || part.ImageURL != "" {
				return errors.New("text part is invalid")
			}
			textParts++
		case ModelContentPartImageURL:
			if part.Text != "" || !validModelImageURL(part.ImageURL) {
				return errors.New("image part is invalid")
			}
			imageParts++
		default:
			return errors.New("content part kind is unsupported")
		}
	}
	if textParts != 1 {
		return errors.New("multimodal content requires exactly one text part")
	}
	if imageParts < 1 || imageParts > maxImageContentParts {
		return errors.New("multimodal content has an invalid image count")
	}
	return nil
}

func validModelImageURL(raw string) bool {
	if raw == "" || len(raw) > maxImageURLBytes ||
		strings.TrimSpace(raw) != raw ||
		strings.ContainsAny(raw, "\r\n\t") {
		return false
	}
	parsed, err := url.ParseRequestURI(raw)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" &&
		parsed.User == nil && parsed.Fragment == ""
}
