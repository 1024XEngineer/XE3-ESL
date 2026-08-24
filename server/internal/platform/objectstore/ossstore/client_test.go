package ossstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/providerobservability"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := fn(request)
	if response != nil && response.Request == nil {
		response.Request = request
	}
	return response, err
}

func TestClientPutSignAndDelete(t *testing.T) {
	payload := []byte("safe audio fixture")
	sum := sha256.Sum256(payload)
	checksum := hex.EncodeToString(sum[:])
	key := "audio/v1/assets/asset_test.wav"
	var methods []string

	httpClient := &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			methods = append(methods, request.Method)
			if request.Host != "speakup-test.oss-cn-shanghai.aliyuncs.com" {
				t.Errorf("unexpected host: %q", request.Host)
			}
			if request.URL.Path != "/"+key {
				t.Errorf("unexpected path: %q", request.URL.Path)
			}

			switch request.Method {
			case http.MethodPut:
				body, err := io.ReadAll(request.Body)
				if err != nil {
					t.Fatalf("read request body: %v", err)
				}
				if !bytes.Equal(body, payload) {
					t.Errorf("unexpected body: %q", body)
				}
				if request.Header.Get("Content-Type") != "audio/wav" ||
					request.Header.Get("Cache-Control") != privateCacheControl ||
					request.Header.Get("X-Oss-Server-Side-Encryption") != serverSideEncryption ||
					request.Header.Get("X-Oss-Forbid-Overwrite") != "true" ||
					request.Header.Get("X-Oss-Object-Acl") != string(aliyunoss.ObjectACLPrivate) ||
					request.Header.Get("X-Oss-Meta-Sha256") != checksum {
					t.Errorf("unexpected put headers: %#v", request.Header)
				}
				return response(http.StatusOK, http.Header{
					"ETag":             []string{`"etag-test"`},
					"X-Oss-Request-Id": []string{"request-put"},
				}, ""), nil
			case http.MethodDelete:
				return response(http.StatusNoContent, http.Header{
					"X-Oss-Request-Id": []string{"request-delete"},
				}, ""), nil
			default:
				t.Fatalf("unexpected method: %s", request.Method)
				return nil, nil
			}
		}),
	}

	client := newTestClient(t, httpClient)
	recorder := &objectProviderRecorder{}
	client.observer = recorder
	putResult, err := client.Put(context.Background(), objectstore.PutRequest{
		Key:            key,
		Body:           bytes.NewReader(payload),
		Size:           int64(len(payload)),
		ContentType:    "audio/wav",
		ChecksumSHA256: checksum,
	})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if putResult.ETag != "etag-test" {
		t.Fatalf("Put() ETag = %q", putResult.ETag)
	}

	beforeSign := time.Now()
	signed, err := client.SignedGet(context.Background(), key)
	if err != nil {
		t.Fatalf("SignedGet() error = %v", err)
	}
	parsedURL, err := url.Parse(signed.URL)
	if err != nil {
		t.Fatalf("parse signed URL: %v", err)
	}
	if parsedURL.Scheme != "https" ||
		parsedURL.Host != "speakup-test.oss-cn-shanghai.aliyuncs.com" ||
		parsedURL.Path != "/"+key ||
		parsedURL.Query().Get("x-oss-signature") == "" {
		t.Fatalf("unexpected signed URL shape: scheme=%q host=%q path=%q signed=%t",
			parsedURL.Scheme,
			parsedURL.Host,
			parsedURL.Path,
			parsedURL.Query().Get("x-oss-signature") != "",
		)
	}
	if signed.ExpiresAt.Before(beforeSign.Add(119*time.Second)) ||
		signed.ExpiresAt.After(beforeSign.Add(121*time.Second)) {
		t.Fatalf("unexpected signed URL expiry: %v", signed.ExpiresAt)
	}

	if err := client.Delete(context.Background(), key); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if strings.Join(methods, ",") != "PUT,DELETE" {
		t.Fatalf("unexpected provider calls: %v", methods)
	}
	putObservation, found := recorder.find(providerobservability.CapabilityObjectPut)
	if !found || putObservation.ErrorKind != providerobservability.ErrorNone ||
		putObservation.Usage.Bytes != float64(len(payload)) {
		t.Fatalf("put observation = %#v, found = %v", putObservation, found)
	}
}

func TestClientPutReconcilesLostResponse(t *testing.T) {
	payload := []byte("replayable audio")
	checksum := sha256Hex(payload)
	key := "audio/v1/assets/lost-response.wav"
	var attempts int

	client := newTestClient(t, &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			switch request.Method {
			case http.MethodPut:
				attempts++
				if attempts == 1 {
					_, _ = io.Copy(io.Discard, request.Body)
					return nil, io.ErrUnexpectedEOF
				}
				return response(
					http.StatusConflict,
					http.Header{"Content-Type": []string{"application/xml"}},
					`<Error><Code>FileAlreadyExists</Code><RequestId>retry-conflict</RequestId></Error>`,
				), nil
			case http.MethodHead:
				return existingObjectResponse(payload, "audio/wav", checksum, "reconciled-etag"), nil
			default:
				t.Fatalf("unexpected method: %s", request.Method)
				return nil, nil
			}
		}),
	})
	recorder := &objectProviderRecorder{}
	client.observer = recorder

	body := bytes.NewReader(payload)
	result, err := client.Put(context.Background(), objectstore.PutRequest{
		Key:            key,
		Body:           body,
		Size:           int64(len(payload)),
		ContentType:    "audio/wav",
		ChecksumSHA256: checksum,
	})
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if attempts != 2 || result.ETag != "reconciled-etag" {
		t.Fatalf("Put() attempts = %d result = %#v", attempts, result)
	}
	if offset, seekErr := body.Seek(0, io.SeekCurrent); seekErr != nil || offset != 0 {
		t.Fatalf("body offset after Put = %d, err = %v", offset, seekErr)
	}
	observation, found := recorder.find(providerobservability.CapabilityObjectPut)
	if !found || observation.ErrorKind != providerobservability.ErrorNone ||
		observation.Usage.Bytes != 0 || recorder.retries != 1 {
		t.Fatalf(
			"reconciled observation = %#v, found = %v, retries = %d",
			observation, found, recorder.retries,
		)
	}
}

func TestClientPutConcurrentSameKeyConverges(t *testing.T) {
	payload := []byte("same concurrent audio")
	checksum := sha256Hex(payload)
	key := "audio/v1/assets/concurrent.wav"
	var mu sync.Mutex
	created := false

	client := newTestClient(t, &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			mu.Lock()
			defer mu.Unlock()
			switch request.Method {
			case http.MethodPut:
				if !created {
					created = true
					return response(http.StatusOK, http.Header{"ETag": []string{`"created-etag"`}}, ""), nil
				}
				return response(
					http.StatusConflict,
					http.Header{"Content-Type": []string{"application/xml"}},
					`<Error><Code>FileAlreadyExists</Code><RequestId>concurrent-conflict</RequestId></Error>`,
				), nil
			case http.MethodHead:
				return existingObjectResponse(payload, "audio/wav", checksum, "created-etag"), nil
			default:
				t.Fatalf("unexpected method: %s", request.Method)
				return nil, nil
			}
		}),
	})

	start := make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			_, err := client.Put(context.Background(), objectstore.PutRequest{
				Key:            key,
				Body:           bytes.NewReader(payload),
				Size:           int64(len(payload)),
				ContentType:    "audio/wav",
				ChecksumSHA256: checksum,
			})
			results <- err
		}()
	}
	close(start)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatalf("concurrent Put() error = %v", err)
		}
	}
}

func TestClientPutPreservesConflictForDifferentObject(t *testing.T) {
	payload := []byte("new audio")
	client := newTestClient(t, &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			switch request.Method {
			case http.MethodPut:
				return response(
					http.StatusConflict,
					http.Header{"Content-Type": []string{"application/xml"}},
					`<Error><Code>FileAlreadyExists</Code><RequestId>different-object</RequestId></Error>`,
				), nil
			case http.MethodHead:
				return existingObjectResponse(
					[]byte("different audio"),
					"audio/wav",
					sha256Hex([]byte("different audio")),
					"different-etag",
				), nil
			default:
				t.Fatalf("unexpected method: %s", request.Method)
				return nil, nil
			}
		}),
	})

	_, err := client.Put(context.Background(), objectstore.PutRequest{
		Key:            "audio/v1/assets/conflict.wav",
		Body:           bytes.NewReader(payload),
		Size:           int64(len(payload)),
		ContentType:    "audio/wav",
		ChecksumSHA256: sha256Hex(payload),
	})
	if !errors.Is(err, objectstore.ErrAlreadyExists) {
		t.Fatalf("Put() error = %v, want ErrAlreadyExists", err)
	}
}

func TestClientPreflightRejectsVersionedBucket(t *testing.T) {
	for _, status := range []string{"Enabled", "Suspended"} {
		t.Run(status, func(t *testing.T) {
			client := newTestClient(t, &http.Client{
				Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
					if request.Method != http.MethodGet {
						t.Fatalf("unexpected preflight request: %s %s", request.Method, request.URL.String())
					}
					switch request.URL.RawQuery {
					case "acl":
						return bucketACLResponse("private"), nil
					case "versioning":
						return response(
							http.StatusOK,
							http.Header{"Content-Type": []string{"application/xml"}},
							"<VersioningConfiguration><Status>"+status+"</Status></VersioningConfiguration>",
						), nil
					default:
						t.Fatalf("unexpected preflight request: %s %s", request.Method, request.URL.String())
						return nil, nil
					}
				}),
			})

			err := client.Preflight(context.Background())
			if !errors.Is(err, ErrBucketVersioningUnsupported) {
				t.Fatalf("Preflight() error = %v", err)
			}
		})
	}
}

func TestClientPreflightRejectsPublicBucketBeforeVersioningCheck(t *testing.T) {
	for _, acl := range []string{"public-read", "public-read-write", ""} {
		t.Run(acl, func(t *testing.T) {
			var requests int
			client := newTestClient(t, &http.Client{
				Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
					requests++
					if request.Method != http.MethodGet || request.URL.RawQuery != "acl" {
						t.Fatalf("unexpected preflight request: %s %s", request.Method, request.URL.String())
					}
					return bucketACLResponse(acl), nil
				}),
			})

			err := client.Preflight(context.Background())
			if !errors.Is(err, ErrBucketACLNotPrivate) {
				t.Fatalf("Preflight() error = %v", err)
			}
			if requests != 1 {
				t.Fatalf("Preflight() requests = %d, want 1", requests)
			}
		})
	}
}

func TestClientPreflightSanitizesBucketACLFailure(t *testing.T) {
	client := newTestClient(t, &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.Method != http.MethodGet || request.URL.RawQuery != "acl" {
				t.Fatalf("unexpected preflight request: %s %s", request.Method, request.URL.String())
			}
			return response(
				http.StatusForbidden,
				http.Header{"Content-Type": []string{"application/xml"}},
				`<Error><Code>AccessDenied</Code><Message>secret-provider-detail</Message><RequestId>safe-acl-request-id</RequestId></Error>`,
			), nil
		}),
	})

	err := client.Preflight(context.Background())
	if !errors.Is(err, objectstore.ErrOperationFailed) ||
		strings.Contains(err.Error(), "secret-provider-detail") ||
		!strings.Contains(err.Error(), "AccessDenied") ||
		!strings.Contains(err.Error(), "safe-acl-request-id") {
		t.Fatalf("unsafe or incomplete Preflight() error: %v", err)
	}
}

func TestClientPreflightAcceptsNeverVersionedBucket(t *testing.T) {
	client := newTestClient(t, &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			switch request.URL.RawQuery {
			case "acl":
				return bucketACLResponse("private"), nil
			case "versioning":
				return response(
					http.StatusOK,
					http.Header{"Content-Type": []string{"application/xml"}},
					"<VersioningConfiguration/>",
				), nil
			default:
				t.Fatalf("unexpected preflight request: %s %s", request.Method, request.URL.String())
				return nil, nil
			}
		}),
	})
	if err := client.Preflight(context.Background()); err != nil {
		t.Fatalf("Preflight() error = %v", err)
	}
}

func TestClientCredentialValidationHonorsContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := newClient(
		ctx,
		config.ObjectStorageConfig{
			Enabled:      true,
			Region:       "cn-shanghai",
			Endpoint:     "https://oss-cn-shanghai.aliyuncs.com",
			Bucket:       "speakup-test",
			AudioPrefix:  "audio/v1",
			ImagePrefix:  "image/v1",
			SignedURLTTL: 2 * time.Minute,
		},
		"audio/v1",
		credentials.CredentialsProviderFunc(func(ctx context.Context) (credentials.Credentials, error) {
			<-ctx.Done()
			return credentials.Credentials{}, ctx.Err()
		}),
		&http.Client{},
		false,
	)
	if !errors.Is(err, objectstore.ErrCredentials) {
		t.Fatalf("newClient() error = %v, want ErrCredentials", err)
	}
}

func TestNewCredentialsProviderUsesEnvironmentOnlyWhenExplicit(t *testing.T) {
	t.Setenv("OSS_ACCESS_KEY_ID", "local-access-key")
	t.Setenv("OSS_ACCESS_KEY_SECRET", "local-secret")
	t.Setenv("OSS_SESSION_TOKEN", "local-session-token")

	provider, err := NewCredentialsProvider(config.ObjectStorageConfig{
		CredentialsProvider: config.ObjectStorageCredentialsEnvironment,
	})
	if err != nil {
		t.Fatalf("NewCredentialsProvider() error = %v", err)
	}
	credential, err := provider.GetCredentials(context.Background())
	if err != nil {
		t.Fatalf("environment provider GetCredentials() error = %v", err)
	}
	if credential.AccessKeyID != "local-access-key" ||
		credential.AccessKeySecret != "local-secret" ||
		credential.SecurityToken != "local-session-token" {
		t.Fatal("environment provider returned unexpected credentials")
	}
}

func TestNewCredentialsProviderDefaultsToECSRoleWithoutEnvironmentFallback(
	t *testing.T,
) {
	t.Setenv("OSS_ACCESS_KEY_ID", "must-not-be-used")
	t.Setenv("OSS_ACCESS_KEY_SECRET", "must-not-be-used")

	provider, err := NewCredentialsProvider(config.ObjectStorageConfig{})
	if err != nil {
		t.Fatalf("NewCredentialsProvider() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	credential, err := provider.GetCredentials(ctx)
	if err == nil ||
		credential.AccessKeyID != "" ||
		credential.AccessKeySecret != "" {
		t.Fatalf("default provider used environment credentials: %#v, %v", credential, err)
	}
}

func TestNewCredentialsProviderRejectsUnknownSource(t *testing.T) {
	provider, err := NewCredentialsProvider(config.ObjectStorageConfig{
		CredentialsProvider: "unknown",
	})
	if provider != nil || !errors.Is(err, objectstore.ErrCredentials) {
		t.Fatalf(
			"NewCredentialsProvider() = %#v, %v, want nil ErrCredentials",
			provider,
			err,
		)
	}
}

func TestClientRejectsCrossPrefixKeyWithoutProviderCall(t *testing.T) {
	called := false
	client := newTestClient(t, &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			called = true
			return response(http.StatusOK, nil, ""), nil
		}),
	})

	_, err := client.Put(context.Background(), objectstore.PutRequest{
		Key:         "../other/asset.wav",
		Body:        bytes.NewReader(nil),
		Size:        0,
		ContentType: "audio/wav",
	})
	if !errors.Is(err, objectstore.ErrInvalidKey) {
		t.Fatalf("Put() error = %v", err)
	}
	if called {
		t.Fatal("provider was called for an invalid key")
	}
}

func TestImageClientIsolatedFromAudioPrefix(t *testing.T) {
	called := false
	client := newTestClientForPrefix(t, &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			called = true
			return response(http.StatusOK, nil, ""), nil
		}),
	}, "image/v1")

	err := client.Delete(
		context.Background(),
		"audio/v1/agent/private.wav",
	)
	if !errors.Is(err, objectstore.ErrInvalidKey) {
		t.Fatalf("Delete() error = %v, want ErrInvalidKey", err)
	}
	if called {
		t.Fatal("image client called provider for an audio key")
	}
}

// TestResumeClientOpensPrivatePDF 验证 Resume OSS 前缀可通过服务端受控流读取 PDF。
func TestResumeClientOpensPrivatePDF(t *testing.T) {
	body := "%PDF-1.4 private resume"
	client := newTestClientForPrefix(t, &http.Client{
		Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.Method != http.MethodGet {
				t.Fatalf("unexpected method: %s", request.Method)
			}
			return response(http.StatusOK, http.Header{
				"Content-Type":   []string{"application/pdf"},
				"Content-Length": []string{fmt.Sprint(len(body))},
			}, body), nil
		}),
	}, "resume/v1")

	reader, err := client.Open(context.Background(), "resume/v1/user/resume.pdf")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer reader.Close()
	read, err := io.ReadAll(reader)
	if err != nil || string(read) != body {
		t.Fatalf("Open() body = %q, error = %v", read, err)
	}
}

func TestClientSanitizesProviderErrors(t *testing.T) {
	client := newTestClient(t, &http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return response(
				http.StatusForbidden,
				http.Header{"Content-Type": []string{"application/xml"}},
				`<Error><Code>AccessDenied</Code><Message>secret-provider-detail</Message><RequestId>safe-request-id</RequestId></Error>`,
			), nil
		}),
	})

	err := client.Delete(context.Background(), "audio/v1/assets/denied.wav")
	if err == nil {
		t.Fatal("Delete() error = nil")
	}
	if strings.Contains(err.Error(), "secret-provider-detail") ||
		!strings.Contains(err.Error(), "AccessDenied") ||
		!strings.Contains(err.Error(), "safe-request-id") {
		t.Fatalf("unsafe or incomplete error: %v", err)
	}
}

func newTestClient(t *testing.T, httpClient *http.Client) *Client {
	return newTestClientForPrefix(t, httpClient, "audio/v1")
}

func newTestClientForPrefix(
	t *testing.T,
	httpClient *http.Client,
	prefix string,
) *Client {
	t.Helper()
	t.Setenv("OSS_ACCESS_KEY_ID", "LTAI-test-access-key")
	t.Setenv("OSS_ACCESS_KEY_SECRET", "test-secret-never-log")

	client, err := newClient(context.Background(), config.ObjectStorageConfig{
		Enabled:      true,
		Region:       "cn-shanghai",
		Endpoint:     "https://oss-cn-shanghai.aliyuncs.com",
		Bucket:       "speakup-test",
		AudioPrefix:  "audio/v1",
		ImagePrefix:  "image/v1",
		ResumePrefix: "resume/v1",
		SignedURLTTL: 2 * time.Minute,
	}, prefix, credentials.NewEnvironmentVariableCredentialsProvider(), httpClient, false)
	if err != nil {
		t.Fatalf("newClient() error = %v", err)
	}
	return client
}

func existingObjectResponse(
	payload []byte,
	contentType string,
	checksum string,
	etag string,
) *http.Response {
	return response(http.StatusOK, http.Header{
		"Content-Length":    []string{fmt.Sprint(len(payload))},
		"Content-Type":      []string{contentType},
		"ETag":              []string{`"` + etag + `"`},
		"X-Oss-Meta-Sha256": []string{checksum},
	}, "")
}

func bucketACLResponse(acl string) *http.Response {
	return response(
		http.StatusOK,
		http.Header{"Content-Type": []string{"application/xml"}},
		"<AccessControlPolicy><AccessControlList><Grant>"+
			acl+
			"</Grant></AccessControlList></AccessControlPolicy>",
	)
}

func sha256Hex(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func response(status int, header http.Header, body string) *http.Response {
	if header == nil {
		header = make(http.Header)
	}
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

type objectProviderRecorder struct {
	observations []providerobservability.Observation
	retries      int
}

func (recorder *objectProviderRecorder) Record(
	observation providerobservability.Observation,
) {
	recorder.observations = append(recorder.observations, observation)
}

func (recorder *objectProviderRecorder) RecordRetry(
	providerobservability.Provider,
	providerobservability.Capability,
) {
	recorder.retries++
}

func (recorder *objectProviderRecorder) find(
	capability providerobservability.Capability,
) (providerobservability.Observation, bool) {
	for _, observation := range recorder.observations {
		if observation.Capability == capability {
			return observation, true
		}
	}
	return providerobservability.Observation{}, false
}
