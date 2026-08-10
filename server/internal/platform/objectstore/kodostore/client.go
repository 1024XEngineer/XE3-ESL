// Package kodostore implements protected object storage with Qiniu Kodo.
package kodostore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"

	qiniuclient "github.com/qiniu/go-sdk/v7/client"
	qiniustorage "github.com/qiniu/go-sdk/v7/storage"
	"github.com/qiniu/go-sdk/v7/storagev2/apis"
	"github.com/qiniu/go-sdk/v7/storagev2/credentials"
	httpclient "github.com/qiniu/go-sdk/v7/storagev2/http_client"
	"github.com/qiniu/go-sdk/v7/storagev2/uploader"
	"github.com/qiniu/go-sdk/v7/storagev2/uptoken"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/config"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
)

const (
	uploadTokenLifetime = 5 * time.Minute
	maxSignedURLTTL     = 2 * time.Minute
	openTimeout         = 30 * time.Second
)

var sha256Pattern = regexp.MustCompile(`\A[0-9a-f]{64}\z`)

var ErrBucketNotPrivate = errors.New("Kodo bucket must be private")

type uploadAPI interface {
	UploadReader(context.Context, io.Reader, *uploader.ObjectOptions, interface{}) error
}

type managementAPI interface {
	GetBucketInfo(context.Context, *apis.GetBucketInfoRequest, *apis.Options) (*apis.GetBucketInfoResponse, error)
	StatObject(context.Context, *apis.StatObjectRequest, *apis.Options) (*apis.StatObjectResponse, error)
	DeleteObject(context.Context, *apis.DeleteObjectRequest, *apis.Options) (*apis.DeleteObjectResponse, error)
}

type Client struct {
	uploader     uploadAPI
	management   managementAPI
	credentials  credentials.CredentialsProvider
	httpClient   *http.Client
	bucket       string
	domain       string
	prefix       string
	signedURLTTL time.Duration
	now          func() time.Time
}

type putResponse struct {
	Hash string `json:"hash"`
	Key  string `json:"key"`
}

func New(
	ctx context.Context,
	storageConfig config.ObjectStorageConfig,
) (*Client, error) {
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
	if ctx == nil {
		ctx = context.Background()
	}
	provider := &credentials.EnvironmentVariableCredentialProvider{}
	if _, err := provider.Get(ctx); err != nil {
		return nil, objectstore.ErrCredentials
	}
	options := httpclient.Options{Credentials: provider}
	client := &Client{
		uploader: uploader.NewFormUploader(&uploader.FormUploaderOptions{
			Options: options,
		}),
		management:   apis.NewStorage(&options),
		credentials:  provider,
		bucket:       storageConfig.Bucket,
		domain:       strings.TrimRight(storageConfig.Domain, "/"),
		prefix:       prefix,
		signedURLTTL: storageConfig.SignedURLTTL,
		now:          time.Now,
		httpClient: &http.Client{
			Timeout: openTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
	if err := client.Preflight(ctx); err != nil {
		return nil, err
	}
	return client, nil
}

func (client *Client) Preflight(ctx context.Context) error {
	if client == nil || client.management == nil || ctx == nil {
		return objectstore.ErrOperationFailed
	}
	result, err := client.management.GetBucketInfo(
		ctx,
		&apis.GetBucketInfoRequest{Bucket: client.bucket},
		nil,
	)
	if err != nil {
		return safeError("preflight", err)
	}
	if result == nil || result.Private != 1 {
		return ErrBucketNotPrivate
	}
	return nil
}

func (client *Client) Put(
	ctx context.Context,
	request objectstore.PutRequest,
) (objectstore.PutResult, error) {
	if client == nil || client.uploader == nil || client.management == nil ||
		client.credentials == nil || ctx == nil {
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
	defer func() {
		_, _ = request.Body.Seek(startOffset, io.SeekStart)
	}()

	policy, err := uptoken.NewPutPolicyWithKey(
		client.bucket,
		request.Key,
		client.now().Add(uploadTokenLifetime),
	)
	if err != nil {
		return objectstore.PutResult{}, objectstore.ErrOperationFailed
	}
	policy.SetInsertOnly(1).SetFsizeMin(request.Size).SetFsizeLimit(request.Size)
	objectName := request.Key
	var response putResponse
	err = client.uploader.UploadReader(
		ctx,
		request.Body,
		&uploader.ObjectOptions{
			UpToken:     uptoken.NewSigner(policy, client.credentials),
			BucketName:  client.bucket,
			ObjectName:  &objectName,
			ContentType: request.ContentType,
			Metadata: map[string]string{
				"sha256": request.ChecksumSHA256,
			},
		},
		&response,
	)
	if err != nil {
		if shouldReconcilePut(err) {
			if existing, matches := client.reconcileExisting(ctx, request); matches {
				return existing, nil
			}
		}
		return objectstore.PutResult{}, safeError("put", err)
	}
	if response.Hash == "" || (response.Key != "" && response.Key != request.Key) {
		return objectstore.PutResult{}, objectstore.ErrOperationFailed
	}
	return objectstore.PutResult{ETag: response.Hash}, nil
}

func shouldReconcilePut(err error) bool {
	var providerError *qiniuclient.ErrorInfo
	if !errors.As(err, &providerError) {
		return true
	}
	return providerError.Code == 614
}

func (client *Client) reconcileExisting(
	ctx context.Context,
	request objectstore.PutRequest,
) (objectstore.PutResult, bool) {
	result, err := client.management.StatObject(
		ctx,
		&apis.StatObjectRequest{Entry: client.bucket + ":" + request.Key},
		nil,
	)
	if err != nil || result == nil ||
		result.Size != request.Size ||
		result.MimeType != request.ContentType ||
		metadataValue(result.Metadata, "sha256") != request.ChecksumSHA256 ||
		result.Hash == "" {
		return objectstore.PutResult{}, false
	}
	return objectstore.PutResult{ETag: result.Hash}, true
}

func metadataValue(metadata map[string]string, key string) string {
	for metadataKey, value := range metadata {
		normalized := strings.TrimPrefix(
			strings.ToLower(metadataKey),
			"x-qn-meta-",
		)
		if normalized == strings.ToLower(key) {
			return value
		}
	}
	return ""
}

func (client *Client) SignedGet(
	ctx context.Context,
	key string,
) (objectstore.SignedGetResult, error) {
	if client == nil || client.credentials == nil || ctx == nil {
		return objectstore.SignedGetResult{}, objectstore.ErrOperationFailed
	}
	if err := objectstore.ValidateKey(client.prefix, key); err != nil {
		return objectstore.SignedGetResult{}, err
	}
	if client.signedURLTTL <= 0 || client.signedURLTTL > maxSignedURLTTL {
		return objectstore.SignedGetResult{}, objectstore.ErrInvalidTTL
	}
	credential, err := client.credentials.Get(ctx)
	if err != nil || credential == nil {
		return objectstore.SignedGetResult{}, objectstore.ErrCredentials
	}
	expiresAt := client.now().Add(client.signedURLTTL)
	return objectstore.SignedGetResult{
		URL: qiniustorage.MakePrivateURLv2(
			credential,
			client.domain,
			key,
			expiresAt.Unix(),
		),
		ExpiresAt: expiresAt,
	}, nil
}

func (client *Client) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	if client == nil || client.httpClient == nil {
		return nil, objectstore.ErrOperationFailed
	}
	signed, err := client.SignedGet(ctx, key)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, signed.URL, nil)
	if err != nil {
		return nil, objectstore.ErrOperationFailed
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return nil, safeError("get", err)
	}
	if response == nil || response.Body == nil {
		return nil, objectstore.ErrOperationFailed
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_ = response.Body.Close()
		return nil, &OperationError{Operation: "get", Code: response.StatusCode}
	}
	mediaType, _, mediaErr := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if mediaErr != nil || mediaType != "application/pdf" {
		_ = response.Body.Close()
		return nil, objectstore.ErrInvalidObject
	}
	return response.Body, nil
}

func (client *Client) Delete(ctx context.Context, key string) error {
	if client == nil || client.management == nil || ctx == nil {
		return objectstore.ErrOperationFailed
	}
	if err := objectstore.ValidateKey(client.prefix, key); err != nil {
		return err
	}
	_, err := client.management.DeleteObject(
		ctx,
		&apis.DeleteObjectRequest{Entry: client.bucket + ":" + key},
		nil,
	)
	if isProviderCode(err, 612) {
		return nil
	}
	if err != nil {
		return safeError("delete", err)
	}
	return nil
}

type OperationError struct {
	Operation  string
	Code       int
	ProviderID string
	RequestID  string
}

func (err *OperationError) Error() string {
	if err.Code == 0 {
		return fmt.Sprintf("%s: %v", err.Operation, objectstore.ErrOperationFailed)
	}
	return fmt.Sprintf(
		"%s: %v (code=%d provider_id=%s request_id=%s)",
		err.Operation,
		objectstore.ErrOperationFailed,
		err.Code,
		err.ProviderID,
		err.RequestID,
	)
}

func (err *OperationError) Unwrap() error {
	if err.Code == 614 {
		return objectstore.ErrAlreadyExists
	}
	return objectstore.ErrOperationFailed
}

func safeError(operation string, err error) error {
	var providerError *qiniuclient.ErrorInfo
	if errors.As(err, &providerError) {
		return &OperationError{
			Operation:  operation,
			Code:       providerError.Code,
			ProviderID: safeIdentifier(providerError.ErrorCode),
			RequestID:  safeIdentifier(providerError.Reqid),
		}
	}
	return &OperationError{Operation: operation}
}

func isProviderCode(err error, code int) bool {
	var providerError *qiniuclient.ErrorInfo
	return errors.As(err, &providerError) && providerError.Code == code
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
