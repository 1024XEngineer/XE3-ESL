package conversation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
	"github.com/gin-gonic/gin"
)

const (
	audioAssetPlaybackPath = "/v1/audio-assets/:audio_asset_id/playback"
	audioAssetDeletePath   = "/v1/audio-assets/:audio_asset_id"
)

// AudioAssetHTTPService is the application boundary consumed by the protected
// playback and deletion routes.
type AudioAssetHTTPService interface {
	Playback(context.Context, AudioAssetActor, string) (objectstore.SignedGetResult, error)
	Delete(context.Context, AudioAssetActor, string) (AudioAsset, error)
}

// AudioAssetActorResolver is implemented by the authentication layer. It must
// return only an Actor derived from a validated server-side Session; route
// parameters, query values, and request bodies are never identity sources.
type AudioAssetActorResolver interface {
	ResolveAudioAssetActor(*http.Request) (AudioAssetActor, bool)
}

// AudioAssetActorResolverFunc adapts an authentication function to
// AudioAssetActorResolver.
type AudioAssetActorResolverFunc func(*http.Request) (AudioAssetActor, bool)

func (resolve AudioAssetActorResolverFunc) ResolveAudioAssetActor(
	request *http.Request,
) (AudioAssetActor, bool) {
	return resolve(request)
}

// RegisterAudioAssetRoutes installs only the two protected routes frozen in
// the Conversation API. The composition root remains responsible for supplying
// the production authentication resolver and service.
func RegisterAudioAssetRoutes(
	routes gin.IRoutes,
	service AudioAssetHTTPService,
	actors AudioAssetActorResolver,
) error {
	if nilDependency(routes) || nilDependency(service) || nilDependency(actors) {
		return ErrAudioAssetInvalidDependency
	}

	handler := audioAssetHTTPHandler{service: service, actors: actors}
	routes.GET(audioAssetPlaybackPath, handler.playback)
	routes.DELETE(audioAssetDeletePath, handler.delete)
	return nil
}

type audioAssetHTTPHandler struct {
	service AudioAssetHTTPService
	actors  AudioAssetActorResolver
}

func (handler audioAssetHTTPHandler) playback(c *gin.Context) {
	actor, ok := handler.resolveActor(c)
	if !ok {
		return
	}
	assetID, ok := handler.resolveAssetID(c)
	if !ok {
		return
	}

	result, err := handler.service.Playback(
		c.Request.Context(),
		actor,
		assetID,
	)
	if err != nil {
		handler.writeServiceError(c, err, true)
		return
	}

	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{
		"playback_url": result.URL,
		"expires_at":   result.ExpiresAt,
	})
}

func (handler audioAssetHTTPHandler) delete(c *gin.Context) {
	actor, ok := handler.resolveActor(c)
	if !ok {
		return
	}
	assetID, ok := handler.resolveAssetID(c)
	if !ok {
		return
	}

	asset, err := handler.service.Delete(
		c.Request.Context(),
		actor,
		assetID,
	)
	if err != nil {
		handler.writeServiceError(c, err, false)
		return
	}
	if asset.Status != AudioAssetDeleted {
		writeAudioAssetHTTPError(
			c,
			http.StatusInternalServerError,
			"internal_error",
			"Internal server error.",
			false,
		)
		return
	}

	c.Status(http.StatusNoContent)
}

func (handler audioAssetHTTPHandler) resolveActor(
	c *gin.Context,
) (AudioAssetActor, bool) {
	actor, ok := handler.actors.ResolveAudioAssetActor(c.Request)
	if !ok ||
		actor.UserID != strings.TrimSpace(actor.UserID) ||
		!validAudioAssetIdentifier(actor.UserID) {
		c.Header("WWW-Authenticate", "Bearer")
		writeAudioAssetHTTPError(
			c,
			http.StatusUnauthorized,
			"authentication_required",
			"Authentication is required.",
			false,
		)
		return AudioAssetActor{}, false
	}
	return actor, true
}

func (audioAssetHTTPHandler) resolveAssetID(c *gin.Context) (string, bool) {
	assetID := c.Param("audio_asset_id")
	if assetID != strings.TrimSpace(assetID) ||
		!validAudioAssetIdentifier(assetID) {
		writeAudioAssetHTTPError(
			c,
			http.StatusBadRequest,
			"invalid_request",
			"Request validation failed.",
			false,
		)
		return "", false
	}
	return assetID, true
}

func (audioAssetHTTPHandler) writeServiceError(
	c *gin.Context,
	err error,
	hideInvalidState bool,
) {
	if errors.Is(err, ErrAudioAssetConcurrentUpdate) ||
		errors.Is(err, ErrAudioAssetCleanupPending) {
		writeAudioAssetHTTPError(
			c,
			http.StatusConflict,
			"resource_conflict",
			"Resource state conflicts with this operation.",
			true,
		)
		return
	}

	notFound := errors.Is(err, ErrAudioAssetNotFound) ||
		errors.Is(err, ErrAudioAssetForbidden) ||
		errors.Is(err, ErrAudioAssetInvalid)
	if hideInvalidState {
		notFound = notFound || errors.Is(err, ErrAudioAssetInvalidTransition)
	}
	if notFound {
		writeAudioAssetHTTPError(
			c,
			http.StatusNotFound,
			"resource_not_found",
			"Resource was not found.",
			false,
		)
		return
	}

	writeAudioAssetHTTPError(
		c,
		http.StatusInternalServerError,
		"internal_error",
		"Internal server error.",
		errors.Is(err, objectstore.ErrOperationFailed),
	)
}

func writeAudioAssetHTTPError(
	c *gin.Context,
	status int,
	code string,
	message string,
	retryable bool,
) {
	c.JSON(status, gin.H{
		"error": gin.H{
			"code":           code,
			"message":        message,
			"retryable":      retryable,
			"correlation_id": newAudioAssetCorrelationID(),
		},
	})
}

func newAudioAssetCorrelationID() string {
	random := make([]byte, 12)
	if _, err := rand.Read(random); err != nil {
		return "corr_audio_asset"
	}
	return "corr_" + hex.EncodeToString(random)
}
