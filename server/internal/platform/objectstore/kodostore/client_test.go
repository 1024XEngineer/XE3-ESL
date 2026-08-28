package kodostore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/providerobservability"
)

var fixedNow = time.Date(2026, 8, 10, 10, 0, 0, 0, time.UTC)

type apiStub struct {
	acl        *s3.GetBucketAclOutput
	aclErr     error
	put        *s3.PutObjectOutput
	putErr     error
	head       *s3.HeadObjectOutput
	headErr    error
	get        *s3.GetObjectOutput
	getErr     error
	deleted    *s3.DeleteObjectInput
	deleteErr  error
	inspectPut func(*s3.PutObjectInput) error
	headCall   func() (*s3.HeadObjectOutput, error)
}

func (stub *apiStub) GetBucketAcl(context.Context, *s3.GetBucketAclInput, ...func(*s3.Options)) (*s3.GetBucketAclOutput, error) {
	return stub.acl, stub.aclErr
}

func (stub *apiStub) PutObject(_ context.Context, input *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	if stub.inspectPut != nil {
		if err := stub.inspectPut(input); err != nil {
			return nil, err
		}
	}
	return stub.put, stub.putErr
}

func (stub *apiStub) HeadObject(context.Context, *s3.HeadObjectInput, ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	if stub.headCall != nil {
		return stub.headCall()
	}
	return stub.head, stub.headErr
}

func (stub *apiStub) GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	return stub.get, stub.getErr
}

func (stub *apiStub) DeleteObject(_ context.Context, input *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	stub.deleted = input
	return &s3.DeleteObjectOutput{}, stub.deleteErr
}

type presignerStub struct {
	request *v4.PresignedHTTPRequest
	err     error
	expires time.Duration
}

func (stub *presignerStub) PresignGetObject(_ context.Context, _ *s3.GetObjectInput, options ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error) {
	settings := s3.PresignOptions{}
	for _, option := range options {
		option(&settings)
	}
	stub.expires = settings.Expires
	return stub.request, stub.err
}

func TestClientPutSignAndDelete(t *testing.T) {
	payload := []byte("safe audio fixture")
	digest := sha256.Sum256(payload)
	checksum := hex.EncodeToString(digest[:])
	key := "audio/v1/assets/asset_test.wav"
	api := &apiStub{
		acl:     &s3.GetBucketAclOutput{},
		put:     &s3.PutObjectOutput{ETag: aws.String("\"kodo-etag\"")},
		headErr: &smithy.GenericAPIError{Code: "NotFound", Message: "not found"},
	}
	api.inspectPut = func(input *s3.PutObjectInput) error {
		body, err := io.ReadAll(input.Body)
		if err != nil || !bytes.Equal(body, payload) {
			t.Fatalf("uploaded body = %q, %v", body, err)
		}
		if aws.ToString(input.Bucket) != "private-bucket" ||
			aws.ToString(input.Key) != key ||
			aws.ToInt64(input.ContentLength) != int64(len(payload)) ||
			aws.ToString(input.ContentType) != "audio/wav" ||
			aws.ToString(input.CacheControl) != "private, no-store" ||
			aws.ToString(input.IfNoneMatch) != "*" ||
			input.Metadata["sha256"] != checksum {
			t.Fatalf("PutObject input = %#v", input)
		}
		return nil
	}
	presigner := &presignerStub{request: &v4.PresignedHTTPRequest{
		URL: "https://s3.cn-east-1.qiniucs.com/private-bucket/" + key + "?X-Amz-Signature=signed",
	}}
	client := newTestClient(api, presigner)
	recorder := &kodoProviderRecorder{}
	client.observer = recorder
	body := bytes.NewReader(payload)

	result, err := client.Put(context.Background(), objectstore.PutRequest{
		Key: key, Body: body, Size: int64(len(payload)),
		ContentType: "audio/wav", ChecksumSHA256: checksum,
	})
	if err != nil || result.ETag != "kodo-etag" {
		t.Fatalf("Put() = %#v, %v", result, err)
	}
	if offset, _ := body.Seek(0, io.SeekCurrent); offset != 0 {
		t.Fatalf("body offset = %d, want 0", offset)
	}

	signed, err := client.SignedGet(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(signed.URL)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Scheme != "https" || parsed.Host != "s3.cn-east-1.qiniucs.com" ||
		parsed.Query().Get("X-Amz-Signature") == "" ||
		presigner.expires != 2*time.Minute ||
		!signed.ExpiresAt.Equal(fixedNow.Add(2*time.Minute)) {
		t.Fatalf("SignedGet() = %#v", signed)
	}

	if err := client.Delete(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	if aws.ToString(api.deleted.Bucket) != "private-bucket" ||
		aws.ToString(api.deleted.Key) != key {
		t.Fatalf("DeleteObject input = %#v", api.deleted)
	}
	if len(recorder.observations) != 2 {
		t.Fatalf("local signing created a provider observation: %#v", recorder.observations)
	}
	for _, capability := range []providerobservability.Capability{
		providerobservability.CapabilityObjectPut,
		providerobservability.CapabilityObjectDelete,
	} {
		observation, found := recorder.find(capability)
		if !found || observation.ErrorKind != providerobservability.ErrorNone {
			t.Fatalf("observation for %q = %#v, found = %v", capability, observation, found)
		}
	}
}

func TestClientReconcilesMatchingExistingObject(t *testing.T) {
	payload := []byte("same object")
	digest := sha256.Sum256(payload)
	checksum := hex.EncodeToString(digest[:])
	api := &apiStub{
		head: &s3.HeadObjectOutput{
			ContentLength: aws.Int64(int64(len(payload))),
			ContentType:   aws.String("audio/wav"),
			ETag:          aws.String("\"existing-etag\""),
			Metadata:      map[string]string{"sha256": checksum},
		},
	}
	client := newTestClient(api, &presignerStub{})
	result, err := client.Put(context.Background(), objectstore.PutRequest{
		Key: "audio/v1/existing.wav", Body: bytes.NewReader(payload),
		Size: int64(len(payload)), ContentType: "audio/wav",
		ChecksumSHA256: checksum,
	})
	if err != nil || result.ETag != "existing-etag" {
		t.Fatalf("Put() = %#v, %v", result, err)
	}
}

func TestClientReconcilesLostResponseAsOneLogicalCall(t *testing.T) {
	payload := []byte("lost response")
	digest := sha256.Sum256(payload)
	checksum := hex.EncodeToString(digest[:])
	headCalls := 0
	stub := &apiStub{putErr: io.ErrUnexpectedEOF}
	stub.headCall = func() (*s3.HeadObjectOutput, error) {
		headCalls++
		if headCalls == 1 {
			return nil, &smithy.GenericAPIError{Code: "NotFound", Message: "not found"}
		}
		return &s3.HeadObjectOutput{
			ContentLength: aws.Int64(int64(len(payload))),
			ContentType:   aws.String("audio/wav"),
			ETag:          aws.String("\"reconciled-etag\""),
			Metadata:      map[string]string{"sha256": checksum},
		}, nil
	}
	client := newTestClient(stub, &presignerStub{})
	recorder := &kodoProviderRecorder{}
	client.observer = recorder
	result, err := client.Put(context.Background(), objectstore.PutRequest{
		Key: "audio/v1/lost.wav", Body: bytes.NewReader(payload),
		Size: int64(len(payload)), ContentType: "audio/wav",
		ChecksumSHA256: checksum,
	})
	if err != nil || result.ETag != "reconciled-etag" || headCalls != 2 {
		t.Fatalf("Put() = %#v, %v; head calls = %d", result, err, headCalls)
	}
	observation, found := recorder.find(providerobservability.CapabilityObjectPut)
	if !found || len(recorder.observations) != 1 ||
		observation.ErrorKind != providerobservability.ErrorNone ||
		observation.Usage.Bytes != 0 {
		t.Fatalf("observations = %#v", recorder.observations)
	}
}

func TestClientRecordsPutOpenDeleteContextOutcome(t *testing.T) {
	for causeName, cause := range map[string]error{
		"cancelled": context.Canceled,
		"timeout":   context.DeadlineExceeded,
	} {
		wantKind := providerobservability.ErrorCancelled
		if errors.Is(cause, context.DeadlineExceeded) {
			wantKind = providerobservability.ErrorTimeout
		}
		for operationName, capability := range map[string]providerobservability.Capability{
			"put":    providerobservability.CapabilityObjectPut,
			"open":   providerobservability.CapabilityObjectOpen,
			"delete": providerobservability.CapabilityObjectDelete,
		} {
			t.Run(causeName+"/"+operationName, func(t *testing.T) {
				stub := &apiStub{
					headErr: &smithy.GenericAPIError{Code: "NotFound", Message: "not found"},
				}
				switch operationName {
				case "put":
					stub.putErr = cause
				case "open":
					stub.getErr = cause
				case "delete":
					stub.deleteErr = cause
				}
				client := newTestClient(stub, &presignerStub{})
				recorder := &kodoProviderRecorder{}
				client.observer = recorder
				var err error
				switch operationName {
				case "put":
					payload := []byte("context outcome")
					digest := sha256.Sum256(payload)
					_, err = client.Put(context.Background(), objectstore.PutRequest{
						Key: "audio/v1/context.wav", Body: bytes.NewReader(payload),
						Size: int64(len(payload)), ContentType: "audio/wav",
						ChecksumSHA256: hex.EncodeToString(digest[:]),
					})
				case "open":
					_, err = client.Open(context.Background(), "audio/v1/context.pdf")
				case "delete":
					err = client.Delete(context.Background(), "audio/v1/context.wav")
				}
				if !errors.Is(err, cause) {
					t.Fatalf("operation error = %v, want context cause %v", err, cause)
				}
				observation, found := recorder.find(capability)
				if !found || len(recorder.observations) != 1 ||
					observation.ErrorKind != wantKind {
					t.Fatalf("observations = %#v", recorder.observations)
				}
			})
		}
	}
}

func TestOperationErrorMapsBoundedProviderCategory(t *testing.T) {
	tests := []struct {
		code   string
		status int
		want   providerobservability.ErrorKind
	}{
		{code: "InvalidAccessKeyId", want: providerobservability.ErrorAuthentication},
		{code: "AccessDenied", want: providerobservability.ErrorAuthorization},
		{status: http.StatusTooManyRequests, want: providerobservability.ErrorRateLimited},
		{status: http.StatusServiceUnavailable, want: providerobservability.ErrorProviderUnavailable},
	}
	for _, test := range tests {
		err := &OperationError{Code: test.code, Status: test.status}
		if got := objectstore.ProviderErrorKind(err); got != test.want {
			t.Fatalf("ProviderErrorKind(%q, %d) = %q, want %q", test.code, test.status, got, test.want)
		}
	}
}

func TestClientRejectsMismatchedExistingObject(t *testing.T) {
	payload := []byte("new object")
	digest := sha256.Sum256(payload)
	client := newTestClient(&apiStub{
		head: &s3.HeadObjectOutput{
			ContentLength: aws.Int64(1), ETag: aws.String("other"),
			ContentType: aws.String("audio/wav"),
		},
	}, &presignerStub{})
	_, err := client.Put(context.Background(), objectstore.PutRequest{
		Key: "audio/v1/existing.wav", Body: bytes.NewReader(payload),
		Size: int64(len(payload)), ContentType: "audio/wav",
		ChecksumSHA256: hex.EncodeToString(digest[:]),
	})
	if !errors.Is(err, objectstore.ErrAlreadyExists) {
		t.Fatalf("Put() error = %v", err)
	}
}

func TestClientPreflightRequiresPrivateBucket(t *testing.T) {
	client := newTestClient(&apiStub{acl: &s3.GetBucketAclOutput{Grants: []types.Grant{{
		Grantee: &types.Grantee{URI: aws.String("http://acs.amazonaws.com/groups/global/AllUsers")},
	}}}}, &presignerStub{})
	if err := client.Preflight(context.Background()); !errors.Is(err, ErrBucketNotPrivate) {
		t.Fatalf("Preflight() error = %v", err)
	}
}

func TestClientPreflightAcceptsPrivateBucket(t *testing.T) {
	client := newTestClient(&apiStub{acl: &s3.GetBucketAclOutput{Grants: []types.Grant{{
		Grantee: &types.Grantee{URI: aws.String("private-owner")},
	}}}}, &presignerStub{})
	if err := client.Preflight(context.Background()); err != nil {
		t.Fatalf("Preflight() error = %v", err)
	}
}

func TestClientOpenReturnsOnlyPDF(t *testing.T) {
	api := &apiStub{get: &s3.GetObjectOutput{
		Body:        io.NopCloser(strings.NewReader("%PDF fixture")),
		ContentType: aws.String("application/pdf"),
	}}
	client := newTestClient(api, &presignerStub{})
	client.prefix = "resume/v1"

	reader, err := client.Open(context.Background(), "resume/v1/document.pdf")
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	content, err := io.ReadAll(reader)
	if err != nil || string(content) != "%PDF fixture" {
		t.Fatalf("Open() content = %q, %v", content, err)
	}
}

func TestNewRejectsMissingCredentialsBeforeNetwork(t *testing.T) {
	t.Setenv("QINIU_ACCESS_KEY", "")
	t.Setenv("QINIU_SECRET_KEY", "")
	client, err := New(context.Background(), config.ObjectStorageConfig{
		Enabled: true, Provider: config.ObjectStorageProviderQiniuKodo,
		Region: "cn-east-1", Endpoint: "https://s3.cn-east-1.qiniucs.com",
		Bucket:      "private-bucket",
		AudioPrefix: "audio/v1", ImagePrefix: "image/v1", ResumePrefix: "resume/v1",
		SignedURLTTL: 2 * time.Minute,
	})
	if client != nil || !errors.Is(err, objectstore.ErrCredentials) {
		t.Fatalf("New() = %#v, %v", client, err)
	}
}

func TestNewForPrefixRejectsInvalidConfigurationBeforeCredentials(t *testing.T) {
	testCases := []struct {
		name          string
		configuration config.ObjectStorageConfig
		prefix        string
		expected      error
	}{
		{
			name: "disabled",
			configuration: config.ObjectStorageConfig{
				Provider: config.ObjectStorageProviderQiniuKodo,
			},
			prefix: "audio/v1", expected: objectstore.ErrDisabled,
		},
		{
			name: "wrong provider",
			configuration: config.ObjectStorageConfig{
				Enabled: true, Provider: config.ObjectStorageProviderAliyunOSS,
			},
			prefix: "audio/v1", expected: objectstore.ErrOperationFailed,
		},
		{
			name: "unknown prefix",
			configuration: config.ObjectStorageConfig{
				Enabled: true, Provider: config.ObjectStorageProviderQiniuKodo,
				AudioPrefix: "audio/v1", ImagePrefix: "image/v1", ResumePrefix: "resume/v1",
			},
			prefix: "other/v1", expected: objectstore.ErrInvalidKey,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			client, err := NewForPrefix(
				context.Background(), testCase.configuration, testCase.prefix,
			)
			if client != nil || !errors.Is(err, testCase.expected) {
				t.Fatalf("NewForPrefix() = %#v, %v; want %v", client, err, testCase.expected)
			}
		})
	}
}

func newTestClient(api s3API, presigner s3Presigner) *Client {
	return &Client{
		api: api, presigner: presigner,
		bucket: "private-bucket", endpointHost: "s3.cn-east-1.qiniucs.com",
		prefix: "audio/v1", signedURLTTL: 2 * time.Minute,
		now: func() time.Time { return fixedNow },
	}
}

type kodoProviderRecorder struct {
	observations []providerobservability.Observation
}

func (recorder *kodoProviderRecorder) Record(
	observation providerobservability.Observation,
) {
	recorder.observations = append(recorder.observations, observation)
}

func (recorder *kodoProviderRecorder) find(
	capability providerobservability.Capability,
) (providerobservability.Observation, bool) {
	for _, observation := range recorder.observations {
		if observation.Capability == capability {
			return observation, true
		}
	}
	return providerobservability.Observation{}, false
}
