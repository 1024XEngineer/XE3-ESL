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
			SignedURLTTL: 2 * time.Minute,
		},
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
	t.Helper()
	t.Setenv("OSS_ACCESS_KEY_ID", "LTAI-test-access-key")
	t.Setenv("OSS_ACCESS_KEY_SECRET", "test-secret-never-log")

	client, err := newClient(context.Background(), config.ObjectStorageConfig{
		Enabled:      true,
		Region:       "cn-shanghai",
		Endpoint:     "https://oss-cn-shanghai.aliyuncs.com",
		Bucket:       "speakup-test",
		AudioPrefix:  "audio/v1",
		SignedURLTTL: 2 * time.Minute,
	}, credentials.NewEnvironmentVariableCredentialsProvider(), httpClient, false)
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
