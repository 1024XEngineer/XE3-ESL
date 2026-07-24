package fake

import (
	"context"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
)

// TextGenerator is an explicit deterministic provider for offline tests and
// local flows. Production assembly must select the Qianwen adapter instead.
type TextGenerator struct {
	result ai.TextResult
	err    error
}

func NewTextGenerator(result ai.TextResult) *TextGenerator {
	return &TextGenerator{result: result}
}

func NewFailingTextGenerator(err error) *TextGenerator {
	return &TextGenerator{err: err}
}

func (generator *TextGenerator) Generate(
	ctx context.Context,
	request ai.TextRequest,
) (ai.TextResult, error) {
	if err := ctx.Err(); err != nil {
		kind := ai.ErrorCancelled
		if err == context.DeadlineExceeded {
			kind = ai.ErrorTimeout
		}
		return ai.TextResult{}, ai.NewGenerationError(kind, 0, "", "", err)
	}
	if err := ai.ValidateTextRequest(request); err != nil {
		return ai.TextResult{}, ai.NewGenerationError(ai.ErrorInvalidRequest, 0, "", "", err)
	}
	if generator.err != nil {
		return ai.TextResult{}, generator.err
	}
	return generator.result, nil
}

var _ ai.TextGenerator = (*TextGenerator)(nil)
