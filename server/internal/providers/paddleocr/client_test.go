package paddleocr

import (
	"context"
	"errors"
	"testing"
	"time"

	paddleapi "github.com/PaddlePaddle/PaddleOCR/api_sdk/go"

	resumeocr "github.com/1024XEngineer/XE3-ESL/server/internal/coaching/preparation/interviewresume/ocr"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/providerobservability"
)

func TestClientMapsDocumentParsingResponseWithoutLeakingURL(t *testing.T) {
	client := testClient()
	client.parse = func(
		_ context.Context,
		request *paddleapi.DocParsingRequest,
	) (*paddleapi.DocParsingResult, error) {
		if request.FileURL != "https://private.example/file.pdf?secret" ||
			request.Model != paddleapi.PaddleOCRVL16 || request.PageRanges != "" {
			t.Fatalf("request = %#v", request)
		}
		return &paddleapi.DocParsingResult{Pages: []paddleapi.DocParsingPage{
			{MarkdownText: "# Backend Engineer\n\nGo and PostgreSQL"},
			{MarkdownText: "## Education\n\nHDU"},
		}}, nil
	}
	result, err := client.RecognizePDF(
		context.Background(),
		"https://private.example/file.pdf?secret",
	)
	if err != nil || result.PageCount != 2 || len(result.Pages) != 2 ||
		result.Pages[0].Words[0].Text != "# Backend Engineer\n\nGo and PostgreSQL" {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
}

func TestClientNormalizesTimeoutProviderAndInvalidOutput(t *testing.T) {
	for name, test := range map[string]struct {
		call parseDocumentCall
		want string
	}{
		"timeout": {
			call: func(context.Context, *paddleapi.DocParsingRequest) (*paddleapi.DocParsingResult, error) {
				return nil, &paddleapi.RequestTimeoutError{}
			},
			want: resumeocr.FailureTimeout,
		},
		"provider": {
			call: func(context.Context, *paddleapi.DocParsingRequest) (*paddleapi.DocParsingResult, error) {
				return nil, errors.New("secret upstream detail")
			},
			want: resumeocr.FailureProvider,
		},
		"output": {
			call: func(context.Context, *paddleapi.DocParsingRequest) (*paddleapi.DocParsingResult, error) {
				return &paddleapi.DocParsingResult{}, nil
			},
			want: resumeocr.FailureOutputInvalid,
		},
	} {
		t.Run(name, func(t *testing.T) {
			client := testClient()
			client.parse = test.call
			_, err := client.RecognizePDF(context.Background(), "https://private.example/file.pdf")
			failure, ok := err.(interface{ FailureCode() string })
			if !ok || failure.FailureCode() != test.want {
				t.Fatalf("error = %v, want code %q", err, test.want)
			}
			if err != nil && stringsContains(err.Error(), "secret upstream detail") {
				t.Fatal("provider detail leaked through normalized error")
			}
		})
	}
}

func TestNewRejectsMissingTokenAndNonDocumentModel(t *testing.T) {
	for name, configuration := range map[string]Config{
		"token": {
			BaseURL: "https://paddleocr.aistudio-app.com",
			Model:   paddleapi.PaddleOCRVL16,
			Timeout: time.Second,
		},
		"model": {
			AccessToken: "test-token",
			BaseURL:     "https://paddleocr.aistudio-app.com",
			Model:       paddleapi.PPOCRv6,
			Timeout:     time.Second,
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := New(configuration); err == nil {
				t.Fatal("New accepted invalid configuration")
			}
		})
	}
}

func TestClientRecordsPagesAndStableFailureOnly(t *testing.T) {
	for name, test := range map[string]struct {
		call      parseDocumentCall
		wantKind  providerobservability.ErrorKind
		wantPages float64
	}{
		"success": {
			call: func(context.Context, *paddleapi.DocParsingRequest) (*paddleapi.DocParsingResult, error) {
				return &paddleapi.DocParsingResult{Pages: []paddleapi.DocParsingPage{
					{MarkdownText: "page one"}, {MarkdownText: "page two"},
				}}, nil
			},
			wantKind: providerobservability.ErrorNone, wantPages: 2,
		},
		"timeout": {
			call: func(context.Context, *paddleapi.DocParsingRequest) (*paddleapi.DocParsingResult, error) {
				return nil, &paddleapi.PollTimeoutError{}
			},
			wantKind: providerobservability.ErrorTimeout,
		},
	} {
		t.Run(name, func(t *testing.T) {
			recorder := &paddleProviderRecorder{}
			client := testClient()
			client.parse = test.call
			client.observer = recorder
			_, _ = client.RecognizePDF(
				context.Background(), "https://private.example/file.pdf?secret",
			)
			if len(recorder.observations) != 1 {
				t.Fatalf("observations = %#v", recorder.observations)
			}
			observation := recorder.observations[0]
			if observation.Provider != providerobservability.ProviderPaddleOCR ||
				observation.Capability != providerobservability.CapabilityDocumentOCR ||
				observation.ErrorKind != test.wantKind ||
				observation.Usage.Pages != test.wantPages {
				t.Fatalf("observation = %#v", observation)
			}
		})
	}
}

type paddleProviderRecorder struct {
	observations []providerobservability.Observation
}

func (recorder *paddleProviderRecorder) Record(
	observation providerobservability.Observation,
) {
	recorder.observations = append(recorder.observations, observation)
}

func (*paddleProviderRecorder) RecordRetry(
	providerobservability.Provider,
	providerobservability.Capability,
) {
}

func testClient() *Client {
	return &Client{config: Config{
		AccessToken: "test-token",
		BaseURL:     "https://paddleocr.aistudio-app.com",
		Model:       paddleapi.PaddleOCRVL16,
		Timeout:     time.Second,
	}}
}

func stringsContains(value, part string) bool {
	for index := 0; index+len(part) <= len(value); index++ {
		if value[index:index+len(part)] == part {
			return true
		}
	}
	return false
}
