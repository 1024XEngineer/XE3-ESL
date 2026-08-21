package context

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

const (
	coachingProfileContextPolicyV1         = "coaching-profile-context-v1"
	coachingProfileContextNotAvailable     = "not_available"
	CoachingProfileContextUnavailableError = "unavailable_error"
	coachingProfileContextDisabled         = "disabled"
	coachingProfileContextSelected         = "selected"
	coachingProfileContextOmittedBudget    = "omitted_budget"
	maxCoachingProfileContextCharacters    = 2048
)

type CoachingProfileContribution struct {
	Content string
	Enabled bool
	Version int64
}

func (contribution CoachingProfileContribution) Valid() bool {
	return contribution.Version >= 0 &&
		contribution.Content == strings.TrimSpace(contribution.Content) &&
		utf8.RuneCountInString(contribution.Content) <=
			maxCoachingProfileContextCharacters
}

type CoachingProfileContributor interface {
	Contribute(
		context.Context,
		requestcontext.Actor,
	) (CoachingProfileContribution, error)
}

func selectCoachingProfileContext(
	systemContent string,
	contribution CoachingProfileContribution,
	systemBudget int,
) (string, string) {
	if !contribution.Enabled {
		return systemContent, coachingProfileContextDisabled
	}
	if contribution.Content == "" {
		return systemContent, coachingProfileContextNotAvailable
	}
	separated := " " + contribution.Content
	if utf8.RuneCountInString(systemContent)+
		utf8.RuneCountInString(separated) > systemBudget {
		return systemContent, coachingProfileContextOmittedBudget
	}
	return systemContent + separated, coachingProfileContextSelected
}
