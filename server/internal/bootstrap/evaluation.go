package bootstrap

import (
	"errors"

	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation"
	evaluationapi "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/api"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/evidence"
	evaluationpostgres "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/postgres"
	"github.com/1024XEngineer/XE3-ESL/server/internal/coaching/evaluation/scoring"
	practicepostgres "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/postgres"
	practicevoicepostgres "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/practice/voice/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

type EvaluationComposition struct {
	interviewCoordinator *scoring.InterviewShadowCoordinator
	ieltsCoordinator     *scoring.IELTSSpeakingShadowCoordinator
	handler              *evaluationapi.HTTPHandler
	worker               scoring.Processor
}

func NewEvaluationComposition(
	database *pgxpool.Pool,
	textGenerator scoring.TextGenerator,
	policies *scoring.EvaluationPolicyRegistry,
	configuration scoring.Configuration,
) (*EvaluationComposition, error) {
	if database == nil || textGenerator == nil || policies == nil {
		return nil, errors.New("bootstrap: Evaluation dependencies are required")
	}
	practiceRepository, err := practicepostgres.New(database)
	if err != nil {
		return nil, err
	}
	voiceRepository, err := practicevoicepostgres.New(database)
	if err != nil {
		return nil, err
	}
	audioRepository, err := practicevoicepostgres.NewAudioAssetRepository(database)
	if err != nil {
		return nil, err
	}
	evidenceSource, err := evidence.NewEvidenceSourceReader(
		practiceRepository,
		voiceRepository,
		audioRepository,
	)
	if err != nil {
		return nil, err
	}
	repository := evaluationpostgres.NewPostgresRepository(database)
	evidenceRepository := evidence.NewPostgresRepository(database)
	evidenceService := evidence.NewEvidenceSnapshotService(
		evidenceSource,
		evidenceRepository,
	)
	evaluationService := evaluation.NewService(repository, evidenceRepository)
	runtime, err := scoring.NewRuntime(
		repository,
		practiceRepository,
		evidenceService,
		evaluationService,
		textGenerator,
		policies,
		configuration,
	)
	if err != nil {
		return nil, err
	}
	application, err := evaluationapi.NewApplication(
		evaluationService,
		repository,
		repository,
		repository,
		runtime.InterviewConfiguration(),
		runtime.IELTSSpeakingConfiguration(),
	)
	if err != nil {
		return nil, err
	}
	handler, err := evaluationapi.NewHTTPHandler(application)
	if err != nil {
		return nil, err
	}
	return &EvaluationComposition{
		interviewCoordinator: runtime.InterviewCoordinator(),
		ieltsCoordinator:     runtime.IELTSSpeakingCoordinator(),
		handler:              handler,
		worker:               runtime.Processor(),
	}, nil
}

func (composition *EvaluationComposition) InterviewShadowCoordinator() *scoring.InterviewShadowCoordinator {
	if composition == nil {
		return nil
	}
	return composition.interviewCoordinator
}

func (composition *EvaluationComposition) IELTSSpeakingShadowCoordinator() *scoring.IELTSSpeakingShadowCoordinator {
	if composition == nil {
		return nil
	}
	return composition.ieltsCoordinator
}

func (composition *EvaluationComposition) HTTPHandler() *evaluationapi.HTTPHandler {
	if composition == nil {
		return nil
	}
	return composition.handler
}

func (composition *EvaluationComposition) Worker() scoring.Processor {
	if composition == nil {
		return nil
	}
	return composition.worker
}
