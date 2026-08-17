package run

import (
	stdcontext "context"
	"encoding/json"

	agentclientaction "github.com/1024XEngineer/XE3-ESL/server/internal/agent/clientaction"
	agentcontext "github.com/1024XEngineer/XE3-ESL/server/internal/agent/context"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type Repository interface {
	CreateInitial(
		stdcontext.Context,
		string,
		string,
		string,
		string,
		Configuration,
	) (Submission, error)
	CreateRetry(stdcontext.Context, string, string, string, Configuration) (Retry, error)
	Claim(stdcontext.Context, string, string) (Run, bool, error)
	Find(stdcontext.Context, string, string) (Run, error)
	SaveContextSnapshot(
		stdcontext.Context,
		string,
		string,
		string,
		agentcontext.Manifest,
	) error
	ProposeToolCall(stdcontext.Context, ToolCall, string) (ToolCall, error)
	StartToolCall(stdcontext.Context, string, string, string, string, string) (ToolCall, error)
	CompleteToolCall(
		stdcontext.Context,
		string,
		string,
		string,
		string,
		json.RawMessage,
		[]ToolSourceRef,
		[]agentclientaction.Action,
	) (ToolCall, error)
	FailToolCall(
		stdcontext.Context,
		string,
		string,
		string,
		string,
		ToolCallStatus,
		string,
	) (ToolCall, error)
	ListClientActions(
		stdcontext.Context,
		string,
		string,
	) ([]agentclientaction.Action, error)
	NewAssistantMessageID() (string, error)
	Complete(
		stdcontext.Context,
		string,
		string,
		string,
		AssistantOutput,
		TextResult,
	) (Run, error)
	Fail(stdcontext.Context, string, string, string, string, bool) (Run, error)
	RecoverInterrupted(stdcontext.Context) (int64, error)
}

// ImageSubmissionRepository owns the atomic creation of a user message with
// image inputs and its initial Run. It is installed only when image input is
// enabled for the process.
type ImageSubmissionRepository interface {
	CreateInitialWithImages(
		stdcontext.Context,
		string,
		string,
		string,
		string,
		[]string,
		Configuration,
	) (Submission, error)
}

type Application interface {
	SubmitText(
		stdcontext.Context,
		requestcontext.Actor,
		string,
		string,
		string,
	) (Submission, error)
	SubmitTextStream(
		stdcontext.Context,
		requestcontext.Actor,
		string,
		string,
		string,
		StreamObserver,
	) (Submission, error)
	SubmitWithImages(
		stdcontext.Context,
		requestcontext.Actor,
		string,
		string,
		string,
		[]string,
	) (Submission, error)
	RetryText(stdcontext.Context, requestcontext.Actor, string, string) (Retry, error)
	RetryTextStream(
		stdcontext.Context,
		requestcontext.Actor,
		string,
		string,
		StreamObserver,
	) (Retry, error)
	GetRun(stdcontext.Context, requestcontext.Actor, string) (Run, error)
	ProcessPending(stdcontext.Context, requestcontext.Actor, Run) (Run, error)
}
