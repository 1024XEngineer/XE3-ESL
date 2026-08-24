package app

import (
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	evaluationapi "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/api"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/ieltsprofile"
	evaluationpostgres "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/postgres"
	evaluationpracticeinteraction "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/practiceturn"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/sessionevaluation"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/speechfeedback"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/textgeneration"
	practiceinteraction "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/interaction"
	practicepostgres "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

type EvaluationConfiguration struct {
	Provider     string
	SessionModel string
	ProfileModel string
	SpeechModel  string
	Worker       evaluation.WorkerConfiguration
}

type PracticeEvaluationSchedulers struct {
	Completion     practicepostgres.CompletionScheduler
	TurnFeedback   practicepostgres.TurnFeedbackScheduler
	IELTSProfile   practicepostgres.IELTSProfileScheduler
	FeedbackReader practiceinteraction.TurnFeedbackStatusReader
}

func (schedulers PracticeEvaluationSchedulers) valid() bool {
	return schedulers.Completion != nil && schedulers.TurnFeedback != nil &&
		schedulers.IELTSProfile != nil &&
		schedulers.FeedbackReader != nil
}

// EvaluationComposition owns the single Evaluation store and exposes narrow
// adapters to Practice, Agent Voice, Review, HTTP, and the two worker lanes.
type EvaluationComposition struct {
	store                 *evaluationpostgres.Store
	handler               *evaluationapi.HTTPHandler
	worker                *evaluation.Worker
	practiceSchedulers    PracticeEvaluationSchedulers
	agentMessageScheduler *evaluation.AgentMessageFeedbackScheduler
}

func NewEvaluationComposition(
	database *pgxpool.Pool,
	sessionGenerator textgeneration.Generator,
	profileGenerator textgeneration.Generator,
	speechGenerator speechfeedback.TextGenerator,
	acoustics evaluation.AcousticEvaluator,
	configuration EvaluationConfiguration,
) (*EvaluationComposition, error) {
	if database == nil || sessionGenerator == nil || profileGenerator == nil ||
		speechGenerator == nil ||
		configuration.Provider == "" || configuration.SessionModel == "" ||
		configuration.ProfileModel == "" ||
		configuration.SpeechModel == "" ||
		!configuration.Worker.Valid() ||
		(configuration.Worker.AcousticsEnabled && acoustics == nil) {
		return nil, errors.New("bootstrap: Evaluation dependencies are required")
	}
	store, err := evaluationpostgres.NewStore(database)
	if err != nil {
		return nil, err
	}
	sessionLineages, err := sessionevaluation.Lineages(
		configuration.Provider,
		configuration.SessionModel,
	)
	if err != nil {
		return nil, err
	}
	speechLineage, err := speechfeedback.Lineage(
		configuration.Provider,
		configuration.SpeechModel,
	)
	if err != nil {
		return nil, err
	}
	profileLineage, err := ieltsprofile.Lineage(
		configuration.Provider, configuration.ProfileModel,
	)
	if err != nil {
		return nil, err
	}
	sessionBuilder, err := evaluation.NewSessionCommandBuilder(
		sessionLineages,
		configuration.Worker.AcousticsEnabled,
	)
	if err != nil {
		return nil, err
	}
	completion, err := evaluationpostgres.NewSessionScheduler(store, sessionBuilder)
	if err != nil {
		return nil, err
	}
	profileBuilder, err := evaluation.NewIELTSProfileCommandBuilder(
		profileLineage, configuration.Worker.AcousticsEnabled,
	)
	if err != nil {
		return nil, err
	}
	profileScheduler, err := evaluationpostgres.NewIELTSProfileScheduler(
		store, profileBuilder,
	)
	if err != nil {
		return nil, err
	}
	turnBuilder, err := evaluation.NewTurnFeedbackCommandBuilder(
		speechLineage,
		configuration.Worker.AcousticsEnabled,
	)
	if err != nil {
		return nil, err
	}
	turnFeedback, err := evaluationpostgres.NewTurnFeedbackScheduler(
		store,
		turnBuilder,
	)
	if err != nil {
		return nil, err
	}
	feedbackReader, err := evaluationpracticeinteraction.NewFeedback(store)
	if err != nil {
		return nil, err
	}
	agentMessage, err := evaluation.NewAgentMessageFeedbackScheduler(
		store,
		speechLineage,
	)
	if err != nil {
		return nil, err
	}
	sessionEvaluator, err := sessionevaluation.New(sessionGenerator)
	if err != nil {
		return nil, err
	}
	profileEvaluator, err := ieltsprofile.New(profileGenerator)
	if err != nil {
		return nil, err
	}
	speechEvaluator, err := speechfeedback.NewCompactEvaluator(speechGenerator)
	if err != nil {
		return nil, err
	}
	worker, err := evaluation.NewWorker(
		store,
		sessionEvaluator,
		profileEvaluator,
		speechEvaluator,
		acoustics,
		store,
		configuration.Worker,
	)
	if err != nil {
		return nil, err
	}
	application, err := evaluationapi.NewApplication(store)
	if err != nil {
		return nil, err
	}
	handler, err := evaluationapi.NewHTTPHandler(application)
	if err != nil {
		return nil, err
	}
	schedulers := PracticeEvaluationSchedulers{
		Completion:     completion,
		TurnFeedback:   turnFeedback,
		IELTSProfile:   profileScheduler,
		FeedbackReader: feedbackReader,
	}
	if !schedulers.valid() {
		return nil, errors.New("bootstrap: Practice Evaluation schedulers are required")
	}
	return &EvaluationComposition{
		store:                 store,
		handler:               handler,
		worker:                worker,
		practiceSchedulers:    schedulers,
		agentMessageScheduler: agentMessage,
	}, nil
}

func (composition *EvaluationComposition) HTTPHandler() *evaluationapi.HTTPHandler {
	if composition == nil {
		return nil
	}
	return composition.handler
}

func (composition *EvaluationComposition) Worker() *evaluation.Worker {
	if composition == nil {
		return nil
	}
	return composition.worker
}

func (composition *EvaluationComposition) PracticeSchedulers() PracticeEvaluationSchedulers {
	if composition == nil {
		return PracticeEvaluationSchedulers{}
	}
	return composition.practiceSchedulers
}

func (composition *EvaluationComposition) AgentMessageScheduler() *evaluation.AgentMessageFeedbackScheduler {
	if composition == nil {
		return nil
	}
	return composition.agentMessageScheduler
}

func (composition *EvaluationComposition) Store() *evaluationpostgres.Store {
	if composition == nil {
		return nil
	}
	return composition.store
}
