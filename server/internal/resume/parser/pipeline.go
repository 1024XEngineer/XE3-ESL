// Package parser 编排 Resume 文档解析与字段提取两个独立阶段。
package parser

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/1024XEngineer/XE3-ESL/server/internal/resume"
	"github.com/1024XEngineer/XE3-ESL/server/internal/resume/document"
	"github.com/1024XEngineer/XE3-ESL/server/internal/resume/fieldextractor"
)

const maximumPipelineVersionBytes = 128

// Pipeline 先还原统一文档，再调用可替换字段提取器生成领域内容。
type Pipeline struct {
	documents document.Parser
	fields    fieldextractor.Extractor
	version   string
}

// NewPipeline 创建两阶段 Resume 解析流水线。
func NewPipeline(
	documents document.Parser,
	fields fieldextractor.Extractor,
) (*Pipeline, error) {
	if documents == nil || fields == nil {
		return nil, errors.New("resume parser pipeline dependencies are required")
	}
	version := documents.Version() + "+" + fields.Version()
	if strings.TrimSpace(documents.Version()) == "" ||
		strings.TrimSpace(fields.Version()) == "" ||
		len(version) > maximumPipelineVersionBytes {
		return nil, errors.New("resume parser pipeline version is invalid")
	}
	return &Pipeline{documents: documents, fields: fields, version: version}, nil
}

// Version 返回同时包含文档解析和字段提取实现的审计版本。
func (pipeline *Pipeline) Version() string {
	if pipeline == nil {
		return ""
	}
	return pipeline.version
}

// Parse 严格按文档解析、字段提取的顺序处理一份简历。
func (pipeline *Pipeline) Parse(
	ctx context.Context,
	reader io.Reader,
) (resume.Content, error) {
	if pipeline == nil || pipeline.documents == nil || pipeline.fields == nil ||
		ctx == nil || reader == nil || ctx.Err() != nil {
		return resume.Content{}, errors.New("resume parser pipeline input is invalid")
	}
	structured, err := pipeline.documents.Parse(ctx, reader)
	if err != nil {
		return resume.Content{}, err
	}
	return pipeline.fields.Extract(ctx, structured)
}
