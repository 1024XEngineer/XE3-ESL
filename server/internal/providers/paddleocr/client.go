// Package paddleocr adapts PaddleOCR's hosted document parsing API to the Resume OCR port.
package paddleocr

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	paddleapi "github.com/PaddlePaddle/PaddleOCR/api_sdk/go"

	resumeocr "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/interviewresume/ocr"
)

// Config contains the server-only PaddleOCR settings.
type Config struct {
	AccessToken string
	BaseURL     string
	Model       string
	Timeout     time.Duration
}

type parseDocumentCall func(
	context.Context,
	*paddleapi.DocParsingRequest,
) (*paddleapi.DocParsingResult, error)

// Client calls PaddleOCR without exposing its response DTOs to Resume.
type Client struct {
	config Config
	parse  parseDocumentCall
}

// New creates a PaddleOCR hosted document parsing client.
func New(configuration Config) (*Client, error) {
	configuration.AccessToken = strings.TrimSpace(configuration.AccessToken)
	configuration.BaseURL = strings.TrimSpace(configuration.BaseURL)
	configuration.Model = strings.TrimSpace(configuration.Model)
	parsedBaseURL, err := url.Parse(configuration.BaseURL)
	if configuration.AccessToken == "" || err != nil || parsedBaseURL.Scheme != "https" ||
		parsedBaseURL.Host == "" || !paddleapi.IsDocumentParsingModel(configuration.Model) ||
		configuration.Timeout <= 0 {
		return nil, errors.New("PaddleOCR Resume OCR configuration is invalid")
	}
	sdkClient, err := paddleapi.NewClient(
		paddleapi.WithToken(configuration.AccessToken),
		paddleapi.WithBaseURL(configuration.BaseURL),
		paddleapi.WithRequestTimeout(configuration.Timeout),
		paddleapi.WithPollTimeout(configuration.Timeout),
		paddleapi.WithClientPlatform("xe3-esl-resume"),
	)
	if err != nil {
		return nil, errors.New("PaddleOCR Resume OCR client initialization failed")
	}
	return &Client{
		config: configuration,
		parse:  sdkClient.ParseDocument,
	}, nil
}

// RecognizePDF parses one remotely readable PDF and returns ordered page text.
func (client *Client) RecognizePDF(
	ctx context.Context,
	sourceURL string,
) (resumeocr.Result, error) {
	if client == nil || client.parse == nil || ctx == nil || ctx.Err() != nil ||
		strings.TrimSpace(sourceURL) == "" {
		return resumeocr.Result{}, resumeocr.NewFailure(resumeocr.FailureProvider)
	}
	callCtx, cancel := context.WithTimeout(ctx, client.config.Timeout)
	defer cancel()
	result, err := client.parse(callCtx, &paddleapi.DocParsingRequest{
		Model:   client.config.Model,
		FileURL: sourceURL,
		Options: &paddleapi.PaddleOCRVLOptions{
			UseDocOrientationClassify: paddleapi.Bool(true),
			UseDocUnwarping:           paddleapi.Bool(true),
			UseLayoutDetection:        paddleapi.Bool(true),
			PrettifyMarkdown:          paddleapi.Bool(true),
			ReturnMarkdownImages:      paddleapi.Bool(false),
		},
	})
	if err != nil {
		return resumeocr.Result{}, mapProviderFailure(err)
	}
	return mapResponse(result)
}

func mapResponse(result *paddleapi.DocParsingResult) (resumeocr.Result, error) {
	if result == nil || len(result.Pages) == 0 {
		return resumeocr.Result{}, resumeocr.NewFailure(resumeocr.FailureOutputInvalid)
	}
	if len(result.Pages) > resumeocr.MaximumRecognizedPages {
		return resumeocr.Result{}, resumeocr.NewFailure(resumeocr.FailurePageLimit)
	}
	pages := make([]resumeocr.Page, 0, len(result.Pages))
	for index, page := range result.Pages {
		text := strings.TrimSpace(page.MarkdownText)
		if text == "" {
			continue
		}
		pages = append(pages, resumeocr.Page{
			Number: index + 1,
			Words:  []resumeocr.Word{{Text: text}},
		})
	}
	if len(pages) == 0 {
		return resumeocr.Result{}, resumeocr.NewFailure(resumeocr.FailureOutputInvalid)
	}
	return resumeocr.Result{PageCount: len(result.Pages), Pages: pages}, nil
}

func mapProviderFailure(err error) error {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return resumeocr.NewFailure(resumeocr.FailureTimeout)
	}
	var requestTimeout *paddleapi.RequestTimeoutError
	var pollTimeout *paddleapi.PollTimeoutError
	if errors.As(err, &requestTimeout) || errors.As(err, &pollTimeout) {
		return resumeocr.NewFailure(resumeocr.FailureTimeout)
	}
	var invalidRequest *paddleapi.InvalidRequestError
	if errors.As(err, &invalidRequest) {
		message := strings.ToLower(invalidRequest.Error())
		if strings.Contains(message, "page") &&
			(strings.Contains(message, "limit") || strings.Contains(message, "range")) {
			return resumeocr.NewFailure(resumeocr.FailurePageLimit)
		}
	}
	return resumeocr.NewFailure(resumeocr.FailureProvider)
}

var _ resumeocr.Client = (*Client)(nil)
