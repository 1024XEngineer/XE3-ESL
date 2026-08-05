package aliyunocr

import (
	"context"
	"errors"
	"testing"
	"time"

	ocr20191230 "github.com/alibabacloud-go/ocr-20191230/v3/client"
	"github.com/alibabacloud-go/tea/tea"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"

	resumeocr "github.com/1024XEngineer/XE3-ESL/server/internal/resume/ocr"
)

func TestClientMapsRecognizePdfResponseWithoutLeakingURL(t *testing.T) {
	client := testClient(t)
	client.recognize = func(
		credential credentials.Credentials,
		sourceURL string,
		configuration Config,
	) (*ocr20191230.RecognizePdfResponse, error) {
		if !credential.HasKeys() || sourceURL != "https://private.example/file.pdf?secret" ||
			configuration.Region != "cn-shanghai" {
			t.Fatalf("call input was not preserved")
		}
		return (&ocr20191230.RecognizePdfResponse{}).
			SetStatusCode(200).
			SetBody((&ocr20191230.RecognizePdfResponseBody{}).SetData(
				(&ocr20191230.RecognizePdfResponseBodyData{}).
					SetPageIndex(1).
					SetWordsInfo([]*ocr20191230.RecognizePdfResponseBodyDataWordsInfo{
						(&ocr20191230.RecognizePdfResponseBodyDataWordsInfo{}).
							SetWord("Backend Engineer").SetX(10).SetY(20),
					}),
			)), nil
	}
	result, err := client.RecognizePDF(
		context.Background(),
		"https://private.example/file.pdf?secret",
	)
	if err != nil || result.PageCount != 1 || len(result.Pages) != 1 ||
		result.Pages[0].Words[0].Text != "Backend Engineer" {
		t.Fatalf("result = %#v, error = %v", result, err)
	}
}

func TestClientNormalizesTimeoutAndProviderFailures(t *testing.T) {
	for name, test := range map[string]struct {
		call recognizeCall
		want string
	}{
		"timeout": {
			call: func(credentials.Credentials, string, Config) (*ocr20191230.RecognizePdfResponse, error) {
				return nil, &tea.SDKError{Code: tea.String("InternalError.Timeout")}
			},
			want: resumeocr.FailureTimeout,
		},
		"provider": {
			call: func(credentials.Credentials, string, Config) (*ocr20191230.RecognizePdfResponse, error) {
				return nil, errors.New("secret upstream detail")
			},
			want: resumeocr.FailureProvider,
		},
	} {
		t.Run(name, func(t *testing.T) {
			client := testClient(t)
			client.recognize = test.call
			_, err := client.RecognizePDF(context.Background(), "https://private.example/file.pdf")
			failure, ok := err.(interface{ FailureCode() string })
			if !ok || failure.FailureCode() != test.want {
				t.Fatalf("error = %v, want code %q", err, test.want)
			}
			if stringsContains(err.Error(), "secret upstream detail") {
				t.Fatal("provider detail leaked through normalized error")
			}
		})
	}
}

func testClient(t *testing.T) *Client {
	t.Helper()
	provider := credentials.CredentialsProviderFunc(func(context.Context) (credentials.Credentials, error) {
		return credentials.Credentials{
			AccessKeyID:     "test-key",
			AccessKeySecret: "test-secret",
		}, nil
	})
	client, err := New(provider, Config{
		Endpoint: "ocr.cn-shanghai.aliyuncs.com",
		Region:   "cn-shanghai",
		Timeout:  time.Second,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return client
}

func stringsContains(value, part string) bool {
	for index := 0; index+len(part) <= len(value); index++ {
		if value[index:index+len(part)] == part {
			return true
		}
	}
	return false
}
