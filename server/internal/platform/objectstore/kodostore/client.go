// Package kodostore implements protected object storage through Qiniu's S3 API.
package kodostore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/providerobservability"
)

const maxSignedURLTTL = 2 * time.Minute

var sha256Pattern = regexp.MustCompile(`\A[0-9a-f]{64}\z`)

var ErrBucketNotPrivate = errors.New("Kodo bucket must be private")

type s3API interface {
	GetBucketAcl(context.Context, *s3.GetBucketAclInput, ...func(*s3.Options)) (*s3.GetBucketAclOutput, error)
	PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	HeadObject(context.Context, *s3.HeadObjectInput, ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
	GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	DeleteObject(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

type s3Presigner interface {
	PresignGetObject(context.Context, *s3.GetObjectInput, ...func(*s3.PresignOptions)) (*v4.PresignedHTTPRequest, error)
}

type Client struct {
	api          s3API
	presigner    s3Presigner
	bucket       string
	endpointHost string
	prefix       string
	signedURLTTL time.Duration
	now          func() time.Time
	observer     providerobservability.Recorder
}

func New(ctx context.Context, storageConfig config.ObjectStorageConfig) (*Client, error) {
	return NewForPrefix(ctx, storageConfig, storageConfig.AudioPrefix)
}

func NewForPrefix(
	ctx context.Context,
	storageConfig config.ObjectStorageConfig,
	prefix string,
) (*Client, error) {
	if !storageConfig.Enabled {
		return nil, objectstore.ErrDisabled
	}
	if storageConfig.Provider != config.ObjectStorageProviderQiniuKodo ||
		!storageConfig.ServerSideEncryption {
		return nil, objectstore.ErrOperationFailed
	}
	if prefix == "" ||
		(prefix != storageConfig.AudioPrefix &&
			prefix != storageConfig.ImagePrefix &&
			prefix != storageConfig.ResumePrefix) {
		return nil, objectstore.ErrInvalidKey
	}
	accessKey := strings.TrimSpace(os.Getenv("QINIU_ACCESS_KEY"))
	secretKey := strings.TrimSpace(os.Getenv("QINIU_SECRET_KEY"))
	if accessKey == "" || secretKey == "" {
		return nil, objectstore.ErrCredentials
	}
	endpoint, err := url.Parse(storageConfig.Endpoint)
	if err != nil || endpoint.Host == "" {
		return nil, objectstore.ErrOperationFailed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	awsConfig := aws.Config{
		Region: storageConfig.Region,
		Credentials: credentials.NewStaticCredentialsProvider(
			accessKey,
			secretKey,
			"",
		),
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
	}
	api := s3.NewFromConfig(awsConfig, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(strings.TrimRight(storageConfig.Endpoint, "/"))
		options.UsePathStyle = true
	})
	client := &Client{
		api:          api,
		presigner:    s3.NewPresignClient(api),
		bucket:       storageConfig.Bucket,
		endpointHost: strings.ToLower(endpoint.Host),
		prefix:       prefix,
		signedURLTTL: storageConfig.SignedURLTTL,
		now:          time.Now,
	}
	if err := client.Preflight(ctx); err != nil {
		return nil, err
	}
	return client, nil
}

// NewForPrefixObserved creates a production client using the service-level
// provider observer.
func NewForPrefixObserved(
	ctx context.Context,
	storageConfig config.ObjectStorageConfig,
	prefix string,
	observer providerobservability.Recorder,
) (*Client, error) {
	if observer == nil {
		return nil, errors.New("Kodo provider observer is required")
	}
	client, err := NewForPrefix(ctx, storageConfig, prefix)
	if err != nil {
		return nil, err
	}
	client.observer = observer
	return client, nil
}

func (client *Client) Preflight(ctx context.Context) error {
	if client == nil || client.api == nil || ctx == nil {
		return objectstore.ErrOperationFailed
	}
	result, err := client.api.GetBucketAcl(ctx, &s3.GetBucketAclInput{
		Bucket: aws.String(client.bucket),
	})
	if err != nil {
		return safeError("preflight", err)
	}
	if result == nil || bucketHasPublicGrant(result.Grants) {
		return ErrBucketNotPrivate
	}
	return nil
}

func bucketHasPublicGrant(grants []types.Grant) bool {
	for _, grant := range grants {
		if grant.Grantee == nil || grant.Grantee.URI == nil {
			continue
		}
		switch *grant.Grantee.URI {
		case "http://acs.amazonaws.com/groups/global/AllUsers",
			"http://acs.amazonaws.com/groups/global/AuthenticatedUsers":
			return true
		}
	}
	return false
}

func (client *Client) Put(
	ctx context.Context,
	request objectstore.PutRequest,
) (callResult objectstore.PutResult, callErr error) {
	if client == nil || client.api == nil || ctx == nil {
		return objectstore.PutResult{}, objectstore.ErrOperationFailed
	}
	if err := objectstore.ValidateKey(client.prefix, request.Key); err != nil {
		return objectstore.PutResult{}, err
	}
	if request.Body == nil || request.Size < 0 ||
		strings.TrimSpace(request.ContentType) == "" ||
		!sha256Pattern.MatchString(request.ChecksumSHA256) {
		return objectstore.PutResult{}, objectstore.ErrInvalidObject
	}
	startOffset, err := request.Body.Seek(0, io.SeekCurrent)
	if err != nil {
		return objectstore.PutResult{}, objectstore.ErrInvalidObject
	}
	defer func() { _, _ = request.Body.Seek(startOffset, io.SeekStart) }()
	startedAt := time.Now()
	storedBytes := int64(0)
	defer func() {
		objectstore.RecordProviderCall(
			client.observer,
			providerobservability.ProviderQiniuKodo,
			providerobservability.CapabilityObjectPut,
			startedAt,
			callErr,
			storedBytes,
		)
	}()

	existing, found, err := client.findExisting(ctx, request)
	if err != nil {
		return objectstore.PutResult{}, err
	}
	if found {
		if existing.ETag != "" {
			return existing, nil
		}
		return objectstore.PutResult{}, objectstore.ErrAlreadyExists
	}

	result, err := client.api.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(client.bucket),
		CacheControl:  aws.String("private, no-store"),
		Key:           aws.String(request.Key),
		Body:          request.Body,
		ContentLength: aws.Int64(request.Size),
		ContentType:   aws.String(request.ContentType),
		IfNoneMatch:   aws.String("*"),
		Metadata:      map[string]string{"sha256": request.ChecksumSHA256},
	})
	if err != nil {
		if client.observer != nil {
			client.observer.RecordRetry(
				providerobservability.ProviderQiniuKodo,
				providerobservability.CapabilityObjectPut,
			)
		}
		if existing, matches := client.reconcileExisting(ctx, request); matches {
			return existing, nil
		}
		return objectstore.PutResult{}, safeError("put", err)
	}
	if result == nil || strings.Trim(aws.ToString(result.ETag), "\"") == "" {
		return objectstore.PutResult{}, objectstore.ErrOperationFailed
	}
	storedBytes = request.Size
	return objectstore.PutResult{ETag: strings.Trim(aws.ToString(result.ETag), "\"")}, nil
}

// Qiniu's S3 compatibility layer currently accepts If-None-Match on PutObject
// without enforcing it. The application already serializes writes per object
// key with durable upload leases; this explicit metadata check also prevents
// ordinary retries from overwriting a different object.
func (client *Client) findExisting(
	ctx context.Context,
	request objectstore.PutRequest,
) (objectstore.PutResult, bool, error) {
	result, err := client.api.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(client.bucket),
		Key:    aws.String(request.Key),
	})
	if isNotFound(err) {
		return objectstore.PutResult{}, false, nil
	}
	if err != nil {
		return objectstore.PutResult{}, false, safeError("head_before_put", err)
	}
	if result == nil {
		return objectstore.PutResult{}, false, objectstore.ErrOperationFailed
	}
	etag := strings.Trim(aws.ToString(result.ETag), "\"")
	if aws.ToInt64(result.ContentLength) == request.Size &&
		aws.ToString(result.ContentType) == request.ContentType &&
		metadataValue(result.Metadata, "sha256") == request.ChecksumSHA256 &&
		etag != "" {
		return objectstore.PutResult{ETag: etag}, true, nil
	}
	return objectstore.PutResult{}, true, nil
}

func (client *Client) reconcileExisting(
	ctx context.Context,
	request objectstore.PutRequest,
) (objectstore.PutResult, bool) {
	result, err := client.api.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(client.bucket),
		Key:    aws.String(request.Key),
	})
	etag := ""
	if result != nil {
		etag = strings.Trim(aws.ToString(result.ETag), "\"")
	}
	if err != nil || result == nil ||
		aws.ToInt64(result.ContentLength) != request.Size ||
		aws.ToString(result.ContentType) != request.ContentType ||
		metadataValue(result.Metadata, "sha256") != request.ChecksumSHA256 ||
		etag == "" {
		return objectstore.PutResult{}, false
	}
	return objectstore.PutResult{ETag: etag}, true
}

func metadataValue(metadata map[string]string, key string) string {
	for metadataKey, value := range metadata {
		if strings.EqualFold(metadataKey, key) {
			return value
		}
	}
	return ""
}

func (client *Client) SignedGet(
	ctx context.Context,
	key string,
) (callResult objectstore.SignedGetResult, callErr error) {
	if client == nil || client.presigner == nil || ctx == nil {
		return objectstore.SignedGetResult{}, objectstore.ErrOperationFailed
	}
	if err := objectstore.ValidateKey(client.prefix, key); err != nil {
		return objectstore.SignedGetResult{}, err
	}
	if client.signedURLTTL <= 0 || client.signedURLTTL > maxSignedURLTTL {
		return objectstore.SignedGetResult{}, objectstore.ErrInvalidTTL
	}
	startedAt := time.Now()
	defer func() {
		objectstore.RecordProviderCall(
			client.observer,
			providerobservability.ProviderQiniuKodo,
			providerobservability.CapabilityObjectSignedGet,
			startedAt,
			callErr,
			0,
		)
	}()
	request, err := client.presigner.PresignGetObject(
		ctx,
		&s3.GetObjectInput{Bucket: aws.String(client.bucket), Key: aws.String(key)},
		func(options *s3.PresignOptions) { options.Expires = client.signedURLTTL },
	)
	if err != nil || request == nil {
		return objectstore.SignedGetResult{}, safeError("sign_get", err)
	}
	parsed, err := url.Parse(request.URL)
	if err != nil || parsed.Scheme != "https" ||
		strings.ToLower(parsed.Host) != client.endpointHost {
		return objectstore.SignedGetResult{}, objectstore.ErrOperationFailed
	}
	return objectstore.SignedGetResult{
		URL:       request.URL,
		ExpiresAt: client.now().Add(client.signedURLTTL),
	}, nil
}

func (client *Client) Open(
	ctx context.Context,
	key string,
) (callResult io.ReadCloser, callErr error) {
	if client == nil || client.api == nil || ctx == nil {
		return nil, objectstore.ErrOperationFailed
	}
	if err := objectstore.ValidateKey(client.prefix, key); err != nil {
		return nil, err
	}
	startedAt := time.Now()
	defer func() {
		objectstore.RecordProviderCall(
			client.observer,
			providerobservability.ProviderQiniuKodo,
			providerobservability.CapabilityObjectOpen,
			startedAt,
			callErr,
			0,
		)
	}()
	result, err := client.api.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(client.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, safeError("get", err)
	}
	if result == nil || result.Body == nil {
		return nil, objectstore.ErrOperationFailed
	}
	mediaType, _, mediaErr := mime.ParseMediaType(aws.ToString(result.ContentType))
	if mediaErr != nil || mediaType != "application/pdf" {
		_ = result.Body.Close()
		return nil, objectstore.ErrInvalidObject
	}
	return result.Body, nil
}

func (client *Client) Delete(ctx context.Context, key string) (callErr error) {
	if client == nil || client.api == nil || ctx == nil {
		return objectstore.ErrOperationFailed
	}
	if err := objectstore.ValidateKey(client.prefix, key); err != nil {
		return err
	}
	startedAt := time.Now()
	defer func() {
		objectstore.RecordProviderCall(
			client.observer,
			providerobservability.ProviderQiniuKodo,
			providerobservability.CapabilityObjectDelete,
			startedAt,
			callErr,
			0,
		)
	}()
	_, err := client.api.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(client.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return safeError("delete", err)
	}
	return nil
}

type OperationError struct {
	Operation string
	Code      string
	Status    int
	RequestID string
}

func (err *OperationError) Error() string {
	if err.Code == "" && err.Status == 0 {
		return fmt.Sprintf("%s: %v", err.Operation, objectstore.ErrOperationFailed)
	}
	return fmt.Sprintf(
		"%s: %v (code=%s status=%d request_id=%s)",
		err.Operation,
		objectstore.ErrOperationFailed,
		err.Code,
		err.Status,
		err.RequestID,
	)
}

func (err *OperationError) Unwrap() error {
	if err.Code == "PreconditionFailed" || err.Status == 412 {
		return objectstore.ErrAlreadyExists
	}
	return objectstore.ErrOperationFailed
}

func safeError(operation string, err error) error {
	if err == nil {
		return &OperationError{Operation: operation}
	}
	result := &OperationError{Operation: operation}
	var apiError smithy.APIError
	if errors.As(err, &apiError) {
		result.Code = safeIdentifier(apiError.ErrorCode())
	}
	var responseError *awshttp.ResponseError
	if errors.As(err, &responseError) {
		result.Status = responseError.HTTPStatusCode()
		result.RequestID = safeIdentifier(responseError.ServiceRequestID())
	}
	return result
}

func isNotFound(err error) bool {
	var apiError smithy.APIError
	if errors.As(err, &apiError) {
		switch apiError.ErrorCode() {
		case "NoSuchKey", "NotFound":
			return true
		}
	}
	var responseError *awshttp.ResponseError
	return errors.As(err, &responseError) && responseError.HTTPStatusCode() == 404
}

func safeIdentifier(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" || len(value) > 128 || strings.IndexFunc(value, func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_')
	}) >= 0 {
		return ""
	}
	return value
}
