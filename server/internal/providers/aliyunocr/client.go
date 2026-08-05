// Package aliyunocr adapts Alibaba Cloud RecognizePdf to the Resume OCR port.
package aliyunocr

import (
	"context"
	"errors"
	"strings"
	"time"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	ocr20191230 "github.com/alibabacloud-go/ocr-20191230/v3/client"
	util "github.com/alibabacloud-go/tea-utils/v2/service"
	"github.com/alibabacloud-go/tea/tea"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"

	resumeocr "github.com/1024XEngineer/XE3-ESL/server/internal/resume/ocr"
)

const connectTimeout = 5 * time.Second

// Config contains non-secret RecognizePdf settings.
type Config struct {
	Endpoint string
	Region   string
	Timeout  time.Duration
}

type recognizeCall func(
	credentials.Credentials,
	string,
	Config,
) (*ocr20191230.RecognizePdfResponse, error)

// Client refreshes the configured RAM credentials for every OCR call.
type Client struct {
	credentials credentials.CredentialsProvider
	config      Config
	recognize   recognizeCall
}

// New creates an Alibaba Cloud RecognizePdf client.
func New(
	provider credentials.CredentialsProvider,
	configuration Config,
) (*Client, error) {
	if provider == nil || strings.TrimSpace(configuration.Endpoint) == "" ||
		strings.TrimSpace(configuration.Region) == "" || configuration.Timeout <= 0 {
		return nil, errors.New("Alibaba Cloud Resume OCR configuration is invalid")
	}
	return &Client{
		credentials: provider,
		config:      configuration,
		recognize:   recognizeWithSDK,
	}, nil
}

// RecognizePDF calls RecognizePdf using a short-lived private OSS URL.
func (client *Client) RecognizePDF(
	ctx context.Context,
	sourceURL string,
) (resumeocr.Result, error) {
	if client == nil || client.credentials == nil || client.recognize == nil ||
		ctx == nil || ctx.Err() != nil || strings.TrimSpace(sourceURL) == "" {
		return resumeocr.Result{}, resumeocr.NewFailure(resumeocr.FailureProvider)
	}
	callCtx, cancel := context.WithTimeout(ctx, client.config.Timeout)
	defer cancel()
	credential, err := client.credentials.GetCredentials(callCtx)
	if err != nil || !credential.HasKeys() || credential.Expired() {
		return resumeocr.Result{}, resumeocr.NewFailure(resumeocr.FailureProvider)
	}
	type callResult struct {
		response *ocr20191230.RecognizePdfResponse
		err      error
	}
	completed := make(chan callResult, 1)
	go func() {
		response, callErr := client.recognize(
			credential,
			sourceURL,
			client.config,
		)
		completed <- callResult{response: response, err: callErr}
	}()
	select {
	case <-callCtx.Done():
		return resumeocr.Result{}, resumeocr.NewFailure(resumeocr.FailureTimeout)
	case result := <-completed:
		if result.err != nil {
			return resumeocr.Result{}, mapProviderFailure(result.err)
		}
		return mapResponse(result.response)
	}
}

func recognizeWithSDK(
	credential credentials.Credentials,
	sourceURL string,
	configuration Config,
) (*ocr20191230.RecognizePdfResponse, error) {
	sdkConfiguration := &openapi.Config{
		AccessKeyId:     tea.String(credential.AccessKeyID),
		AccessKeySecret: tea.String(credential.AccessKeySecret),
		SecurityToken:   tea.String(credential.SecurityToken),
		Endpoint:        tea.String(configuration.Endpoint),
		RegionId:        tea.String(configuration.Region),
	}
	client, err := ocr20191230.NewClient(sdkConfiguration)
	if err != nil {
		return nil, err
	}
	timeoutMilliseconds := int(configuration.Timeout.Milliseconds())
	runtime := (&util.RuntimeOptions{}).
		SetAutoretry(false).
		SetConnectTimeout(int(connectTimeout.Milliseconds())).
		SetReadTimeout(timeoutMilliseconds)
	return client.RecognizePdfWithOptions(
		(&ocr20191230.RecognizePdfRequest{}).SetFileURL(sourceURL),
		runtime,
	)
}

func mapResponse(response *ocr20191230.RecognizePdfResponse) (resumeocr.Result, error) {
	if response == nil || response.StatusCode == nil || *response.StatusCode < 200 ||
		*response.StatusCode >= 300 || response.Body == nil || response.Body.Data == nil {
		return resumeocr.Result{}, resumeocr.NewFailure(resumeocr.FailureOutputInvalid)
	}
	data := response.Body.Data
	pageCount := int(tea.Int64Value(data.PageIndex))
	if pageCount < 1 {
		pageCount = 1
	}
	if pageCount > resumeocr.MaximumRecognizedPages {
		return resumeocr.Result{}, resumeocr.NewFailure(resumeocr.FailurePageLimit)
	}
	words := make([]resumeocr.Word, 0, len(data.WordsInfo))
	for _, item := range data.WordsInfo {
		if item == nil {
			continue
		}
		words = append(words, resumeocr.Word{
			Text:   tea.StringValue(item.Word),
			X:      tea.Int64Value(item.X),
			Y:      tea.Int64Value(item.Y),
			Width:  tea.Int64Value(item.Width),
			Height: tea.Int64Value(item.Height),
		})
	}
	return resumeocr.Result{
		PageCount: pageCount,
		Pages: []resumeocr.Page{{
			Number: 1,
			Words:  words,
		}},
	}, nil
}

func mapProviderFailure(err error) error {
	var sdkError *tea.SDKError
	if errors.As(err, &sdkError) {
		code := strings.ToLower(tea.StringValue(sdkError.Code))
		message := strings.ToLower(tea.StringValue(sdkError.Message))
		combined := code + " " + message
		if strings.Contains(combined, "timeout") {
			return resumeocr.NewFailure(resumeocr.FailureTimeout)
		}
		if strings.Contains(combined, "page") &&
			(strings.Contains(combined, "limit") || strings.Contains(combined, "exceed")) {
			return resumeocr.NewFailure(resumeocr.FailurePageLimit)
		}
	}
	return resumeocr.NewFailure(resumeocr.FailureProvider)
}

var _ resumeocr.Client = (*Client)(nil)
