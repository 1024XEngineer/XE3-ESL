// This file assembles the optional InterviewPreparation PDF parser.
package main

import (
	"context"
	"errors"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/app"
	interviewresume "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/interviewresume"
	resumedocument "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/interviewresume/document"
	resumefieldextractor "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/interviewresume/fieldextractor"
	resumeparser "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/interviewresume/parser"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
	"github.com/1024XEngineer/XE3-ESL/server/internal/providers/paddleocr"
)

func buildInterviewResumeConfiguration(
	ctx context.Context,
	storageConfig config.ObjectStorageConfig,
	textConfig config.TextGenerationConfig,
	ocrConfig config.ResumeOCRConfig,
) (*app.InterviewResumeConfiguration, error) {
	if !storageConfig.Enabled {
		if ocrConfig.Enabled {
			return nil, errors.New("Interview Resume OCR requires private object storage")
		}
		return nil, nil
	}
	generator, err := app.NewResumeFieldGenerator(textConfig)
	if err != nil {
		return nil, err
	}
	fields, err := resumefieldextractor.NewLLMExtractor(
		generator,
		resumefieldextractor.Config{
			Provider: textConfig.Provider, Model: textConfig.Model,
			MaxDocumentCharacters: textConfig.MaxContextChars,
		},
	)
	if err != nil {
		return nil, err
	}
	var pipeline interviewresume.Parser
	if ocrConfig.Enabled {
		client, clientErr := paddleocr.New(paddleocr.Config{
			AccessToken: ocrConfig.AccessToken, BaseURL: ocrConfig.BaseURL,
			Model: ocrConfig.Model, Timeout: ocrConfig.Timeout,
		})
		if clientErr != nil {
			return nil, clientErr
		}
		ocrDocuments, parserErr := resumedocument.NewOCRPDFParser(client)
		if parserErr != nil {
			return nil, parserErr
		}
		pipeline, err = resumeparser.NewOCRFallbackPipeline(
			resumedocument.NewTextPDFParser(), ocrDocuments, fields,
		)
	} else {
		pipeline, err = resumeparser.NewPipeline(
			resumedocument.NewTextPDFParser(), fields,
		)
	}
	if err != nil {
		return nil, err
	}
	store, err := newProtectedObjectStore(
		ctx, storageConfig, storageConfig.ResumePrefix,
	)
	if err != nil {
		return nil, err
	}
	return &app.InterviewResumeConfiguration{
		ObjectStore: store,
		Parser:      pipeline,
		UploadLease: 2 * time.Minute,
	}, nil
}
