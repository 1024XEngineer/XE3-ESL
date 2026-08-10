// 本文件组装 Resume 私有 OSS、PDF 解析器和运行时生命周期。
package main

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/1024XEngineer/XE3-ESL/server/internal/bootstrap"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
	"github.com/1024XEngineer/XE3-ESL/server/internal/providers/paddleocr"
	resumeapp "github.com/1024XEngineer/XE3-ESL/server/internal/resume/app"
	resumedocument "github.com/1024XEngineer/XE3-ESL/server/internal/resume/document"
	resumefieldextractor "github.com/1024XEngineer/XE3-ESL/server/internal/resume/fieldextractor"
	resumeidentifier "github.com/1024XEngineer/XE3-ESL/server/internal/resume/identifier"
	resumeparser "github.com/1024XEngineer/XE3-ESL/server/internal/resume/parser"
	resumestorage "github.com/1024XEngineer/XE3-ESL/server/internal/resume/storage"
)

const resumeParsePollInterval = 2 * time.Second

// buildResumeComposition 在启用私有对象存储时组装完整 Resume 模块。
func buildResumeComposition(
	ctx context.Context,
	pool *pgxpool.Pool,
	storageConfig config.ObjectStorageConfig,
	textConfig config.TextGenerationConfig,
	ocrConfig config.ResumeOCRConfig,
) (*bootstrap.ResumeComposition, error) {
	if !storageConfig.Enabled {
		if ocrConfig.Enabled {
			return nil, errors.New("Resume OCR requires private object storage")
		}
		return nil, nil
	}
	fieldGenerator, err := bootstrap.NewResumeFieldGenerator(textConfig)
	if err != nil {
		return nil, err
	}
	fields, err := resumefieldextractor.NewLLMExtractor(
		fieldGenerator,
		resumefieldextractor.Config{
			Provider:              textConfig.Provider,
			Model:                 textConfig.Model,
			MaxDocumentCharacters: textConfig.MaxContextChars,
		},
	)
	if err != nil {
		return nil, err
	}
	var resumePipeline resumeapp.Parser
	if ocrConfig.Enabled {
		ocrClient, clientErr := paddleocr.New(paddleocr.Config{
			AccessToken: ocrConfig.AccessToken,
			BaseURL:     ocrConfig.BaseURL,
			Model:       ocrConfig.Model,
			Timeout:     ocrConfig.Timeout,
		})
		if clientErr != nil {
			return nil, clientErr
		}
		ocrDocuments, parserErr := resumedocument.NewOCRPDFParser(ocrClient)
		if parserErr != nil {
			return nil, parserErr
		}
		resumePipeline, err = resumeparser.NewOCRFallbackPipeline(
			resumedocument.NewTextPDFParser(),
			ocrDocuments,
			fields,
		)
	} else {
		resumePipeline, err = resumeparser.NewPipeline(
			resumedocument.NewTextPDFParser(),
			fields,
		)
	}
	if err != nil {
		return nil, err
	}
	store, err := newProtectedObjectStore(
		ctx,
		storageConfig,
		storageConfig.ResumePrefix,
	)
	if err != nil {
		return nil, err
	}
	files, err := resumestorage.NewObjectStore(store)
	if err != nil {
		return nil, err
	}
	ids, err := resumeidentifier.NewGenerator(storageConfig.ResumePrefix)
	if err != nil {
		return nil, err
	}
	return bootstrap.NewResumeComposition(
		pool,
		files,
		resumePipeline,
		ids,
		bootstrap.ResumeConfiguration{
			MaximumFileBytes:  10 * 1024 * 1024,
			ParsePollInterval: resumeParsePollInterval,
			ReadURLLifetime:   storageConfig.SignedURLTTL,
		},
	)
}
