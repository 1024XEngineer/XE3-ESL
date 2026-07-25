// Package ossstore implements protected object storage with Alibaba Cloud OSS.
package ossstore

import (
	"context"
	"errors"
	"fmt"
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
	serverSideEncryption       = "AES256"
	privateCacheControl        = "private, no-store"
	configuredConnectTimeout   = 5 * time.Second
	configuredReadWriteTimeout = 30 * time.Second
)

var sha256Pattern = regexp.MustCompile(`\A[0-9a-f]{64}\z`)

type Client struct {
	sdk          *aliyunoss.Client
	bucket       string
	prefix       string
	signedURLTTL time.Duration
}

// NewFromEnvironment creates a client whose AccessKey is read by the official
// SDK from OSS_ACCESS_KEY_ID and OSS_ACCESS_KEY_SECRET.
func NewFromEnvironment(storageConfig config.ObjectStorageConfig) (*Client, error) {
	return newClient(
		storageConfig,
		credentials.NewEnvironmentVariableCredentialsProvider(),
		nil,
	)
}

func newClient(
	storageConfig config.ObjectStorageConfig,
	provider credentials.CredentialsProvider,
	httpClient *http.Client,
) (*Client, error) {
	if !storageConfig.Enabled {
		return nil, objectstore.ErrDisabled
	}
	if provider == nil {
		return nil, objectstore.ErrCredentials
	}
	if _, err := provider.GetCredentials(context.Background()); err != nil {
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

	return &Client{
		sdk:          aliyunoss.NewClient(sdkConfig),
		bucket:       storageConfig.Bucket,
		prefix:       storageConfig.AudioPrefix,
		signedURLTTL: storageConfig.SignedURLTTL,
	}, nil
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
		(request.ChecksumSHA256 != "" && !sha256Pattern.MatchString(request.ChecksumSHA256)) {
		return objectstore.PutResult{}, objectstore.ErrInvalidObject
	}

	metadata := make(map[string]string)
	if request.ChecksumSHA256 != "" {
		metadata["sha256"] = request.ChecksumSHA256
	}

	result, err := c.sdk.PutObject(ctx, &aliyunoss.PutObjectRequest{
		Bucket:               aliyunoss.Ptr(c.bucket),
		Key:                  aliyunoss.Ptr(request.Key),
		Body:                 request.Body,
		ContentLength:        aliyunoss.Ptr(request.Size),
		ContentType:          aliyunoss.Ptr(request.ContentType),
		CacheControl:         aliyunoss.Ptr(privateCacheControl),
		ForbidOverwrite:      aliyunoss.Ptr("true"),
		ServerSideEncryption: aliyunoss.Ptr(serverSideEncryption),
		Metadata:             metadata,
	})
	if err != nil {
		return objectstore.PutResult{}, safeError("put", err)
	}

	return objectstore.PutResult{ETag: strings.Trim(aliyunoss.ToString(result.ETag), `"`)}, nil
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
