// Package ossstore implements protected object storage with Alibaba Cloud OSS.
package ossstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	aliyunoss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
)

const (
	serverSideEncryption        = "AES256"
	privateCacheControl         = "private, no-store"
	configuredConnectTimeout    = 5 * time.Second
	configuredReadWriteTimeout  = 30 * time.Second
	credentialValidationTimeout = 5 * time.Second
	preflightTimeout            = 10 * time.Second
)

var sha256Pattern = regexp.MustCompile(`\A[0-9a-f]{64}\z`)

var (
	ErrBucketACLNotPrivate         = errors.New("OSS bucket ACL must be private")
	ErrBucketVersioningUnsupported = errors.New("OSS bucket versioning must never have been enabled")
)

type Client struct {
	sdk          *aliyunoss.Client
	bucket       string
	prefix       string
	signedURLTTL time.Duration
}

// NewFromEnvironment is intended for explicit local and CI use. It reads an
// AccessKey or STS tuple from OSS_ACCESS_KEY_ID, OSS_ACCESS_KEY_SECRET, and
// optionally OSS_SESSION_TOKEN.
func NewFromEnvironment(
	ctx context.Context,
	storageConfig config.ObjectStorageConfig,
) (*Client, error) {
	return New(
		ctx,
		storageConfig,
		credentials.NewEnvironmentVariableCredentialsProvider(),
	)
}

// NewCredentialsProvider selects the explicitly configured server credential
// source. ECS RAM role credentials are temporary and refreshed by the SDK.
// Environment credentials are reserved for explicit local and CI use.
func NewCredentialsProvider(
	storageConfig config.ObjectStorageConfig,
) (credentials.CredentialsProvider, error) {
	switch storageConfig.CredentialsProvider {
	case "", config.ObjectStorageCredentialsECSRole:
		if storageConfig.RAMRoleName == "" {
			return credentials.NewEcsRoleCredentialsProvider(), nil
		}
		return credentials.NewEcsRoleCredentialsProvider(
			credentials.EcsRamRole(storageConfig.RAMRoleName),
		), nil
	case config.ObjectStorageCredentialsEnvironment:
		return credentials.NewEnvironmentVariableCredentialsProvider(), nil
	default:
		return nil, objectstore.ErrCredentials
	}
}

// New creates a client with an explicitly selected credentials provider. This
// is the production boundary for workload RAM role and refreshing providers.
func New(
	ctx context.Context,
	storageConfig config.ObjectStorageConfig,
	provider credentials.CredentialsProvider,
) (*Client, error) {
	return newClient(
		ctx,
		storageConfig,
		provider,
		nil,
		true,
	)
}

func newClient(
	ctx context.Context,
	storageConfig config.ObjectStorageConfig,
	provider credentials.CredentialsProvider,
	httpClient *http.Client,
	runPreflight bool,
) (*Client, error) {
	if !storageConfig.Enabled {
		return nil, objectstore.ErrDisabled
	}
	if provider == nil {
		return nil, objectstore.ErrCredentials
	}
	if ctx == nil {
		ctx = context.Background()
	}
	credentialCtx, credentialCancel := context.WithTimeout(ctx, credentialValidationTimeout)
	defer credentialCancel()
	if _, err := provider.GetCredentials(credentialCtx); err != nil {
		return nil, objectstore.ErrCredentials
	}

	sdkConfig := aliyunoss.LoadDefaultConfig().
		WithCredentialsProvider(provider).
		WithRegion(storageConfig.Region).
		WithEndpoint(storageConfig.Endpoint).
		WithConnectTimeout(configuredConnectTimeout).
		WithReadWriteTimeout(configuredReadWriteTimeout)
	if httpClient != nil {
		sdkConfig.WithHttpClient(httpClient)
	}

	client := &Client{
		sdk:          aliyunoss.NewClient(sdkConfig),
		bucket:       storageConfig.Bucket,
		prefix:       storageConfig.AudioPrefix,
		signedURLTTL: storageConfig.SignedURLTTL,
	}
	if runPreflight {
		preflightCtx, preflightCancel := context.WithTimeout(ctx, preflightTimeout)
		defer preflightCancel()
		if err := client.Preflight(preflightCtx); err != nil {
			return nil, err
		}
	}
	return client, nil
}

// Preflight rejects a public bucket before any recording can be written and
// rejects versioning states that would defeat physical deletion. An empty
// versioning status means versioning was never set.
func (c *Client) Preflight(ctx context.Context) error {
	acl, err := c.sdk.GetBucketAcl(ctx, &aliyunoss.GetBucketAclRequest{
		Bucket: aliyunoss.Ptr(c.bucket),
	})
	if err != nil {
		return safeError("preflight_acl", err)
	}
	if strings.TrimSpace(aliyunoss.ToString(acl.ACL)) !=
		string(aliyunoss.BucketACLPrivate) {
		return ErrBucketACLNotPrivate
	}

	result, err := c.sdk.GetBucketVersioning(ctx, &aliyunoss.GetBucketVersioningRequest{
		Bucket: aliyunoss.Ptr(c.bucket),
	})
	if err != nil {
		return safeError("preflight_versioning", err)
	}
	if status := strings.TrimSpace(aliyunoss.ToString(result.VersionStatus)); status != "" {
		return ErrBucketVersioningUnsupported
	}
	return nil
}

func (c *Client) Put(
	ctx context.Context,
	request objectstore.PutRequest,
) (objectstore.PutResult, error) {
	if err := objectstore.ValidateKey(c.prefix, request.Key); err != nil {
		return objectstore.PutResult{}, err
	}
	if request.Body == nil ||
		request.Size < 0 ||
		strings.TrimSpace(request.ContentType) == "" ||
		!sha256Pattern.MatchString(request.ChecksumSHA256) {
		return objectstore.PutResult{}, objectstore.ErrInvalidObject
	}

	startOffset, err := request.Body.Seek(0, io.SeekCurrent)
	if err != nil {
		return objectstore.PutResult{}, objectstore.ErrInvalidObject
	}
	defer func() {
		_, _ = request.Body.Seek(startOffset, io.SeekStart)
	}()

	result, err := c.sdk.PutObject(ctx, &aliyunoss.PutObjectRequest{
		Bucket:               aliyunoss.Ptr(c.bucket),
		Key:                  aliyunoss.Ptr(request.Key),
		Body:                 request.Body,
		ContentLength:        aliyunoss.Ptr(request.Size),
		ContentType:          aliyunoss.Ptr(request.ContentType),
		CacheControl:         aliyunoss.Ptr(privateCacheControl),
		ForbidOverwrite:      aliyunoss.Ptr("true"),
		ServerSideEncryption: aliyunoss.Ptr(serverSideEncryption),
		Acl:                  aliyunoss.ObjectACLPrivate,
		Metadata:             map[string]string{"sha256": request.ChecksumSHA256},
	})
	if err != nil {
		if shouldReconcilePut(err) {
			if existing, matches := c.reconcileExisting(ctx, request); matches {
				return existing, nil
			}
		}
		return objectstore.PutResult{}, safeError("put", err)
	}

	return objectstore.PutResult{ETag: strings.Trim(aliyunoss.ToString(result.ETag), `"`)}, nil
}

func shouldReconcilePut(err error) bool {
	var serviceError *aliyunoss.ServiceError
	if !errors.As(err, &serviceError) {
		return true
	}
	return serviceError.Code == "FileAlreadyExists"
}

func (c *Client) reconcileExisting(
	ctx context.Context,
	request objectstore.PutRequest,
) (objectstore.PutResult, bool) {
	result, err := c.sdk.HeadObject(ctx, &aliyunoss.HeadObjectRequest{
		Bucket: aliyunoss.Ptr(c.bucket),
		Key:    aliyunoss.Ptr(request.Key),
	})
	if err != nil ||
		result.ContentLength != request.Size ||
		aliyunoss.ToString(result.ContentType) != request.ContentType ||
		metadataValue(result.Metadata, "sha256") != request.ChecksumSHA256 {
		return objectstore.PutResult{}, false
	}
	return objectstore.PutResult{
		ETag: strings.Trim(aliyunoss.ToString(result.ETag), `"`),
	}, true
}

func metadataValue(metadata map[string]string, key string) string {
	for metadataKey, value := range metadata {
		if strings.EqualFold(metadataKey, key) {
			return value
		}
	}
	return ""
}

func (c *Client) SignedGet(
	ctx context.Context,
	key string,
) (objectstore.SignedGetResult, error) {
	if err := objectstore.ValidateKey(c.prefix, key); err != nil {
		return objectstore.SignedGetResult{}, err
	}
	if c.signedURLTTL <= 0 || c.signedURLTTL > 2*time.Minute {
		return objectstore.SignedGetResult{}, objectstore.ErrInvalidTTL
	}

	result, err := c.sdk.Presign(
		ctx,
		&aliyunoss.GetObjectRequest{
			Bucket: aliyunoss.Ptr(c.bucket),
			Key:    aliyunoss.Ptr(key),
		},
		aliyunoss.PresignExpires(c.signedURLTTL),
	)
	if err != nil {
		return objectstore.SignedGetResult{}, safeError("signed_get", err)
	}

	return objectstore.SignedGetResult{
		URL:       result.URL,
		ExpiresAt: result.Expiration,
	}, nil
}

func (c *Client) Delete(ctx context.Context, key string) error {
	if err := objectstore.ValidateKey(c.prefix, key); err != nil {
		return err
	}

	_, err := c.sdk.DeleteObject(ctx, &aliyunoss.DeleteObjectRequest{
		Bucket: aliyunoss.Ptr(c.bucket),
		Key:    aliyunoss.Ptr(key),
	})
	if err != nil {
		return safeError("delete", err)
	}
	return nil
}

type OperationError struct {
	Operation  string
	Code       string
	StatusCode int
	RequestID  string
}

func (e *OperationError) Error() string {
	if e.Code == "" {
		return fmt.Sprintf("%s: %v", e.Operation, objectstore.ErrOperationFailed)
	}
	return fmt.Sprintf(
		"%s: %v (code=%s status=%d request_id=%s)",
		e.Operation,
		objectstore.ErrOperationFailed,
		e.Code,
		e.StatusCode,
		e.RequestID,
	)
}

func (e *OperationError) Unwrap() error {
	if e.Code == "FileAlreadyExists" {
		return objectstore.ErrAlreadyExists
	}
	return objectstore.ErrOperationFailed
}

func safeError(operation string, err error) error {
	var serviceError *aliyunoss.ServiceError
	if errors.As(err, &serviceError) {
		return &OperationError{
			Operation:  operation,
			Code:       serviceError.Code,
			StatusCode: serviceError.StatusCode,
			RequestID:  serviceError.RequestID,
		}
	}
	return &OperationError{Operation: operation}
}
