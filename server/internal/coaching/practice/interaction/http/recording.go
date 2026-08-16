package voicehttp

import (
	"context"
	"net/http"

	sharedmedia "github.com/1024XEngineer/XE3-ESL/server/internal/media"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
	"github.com/gin-gonic/gin"
)

const (
	recordingPlaybackPath = "/v1/audio-assets/:audio_asset_id/playback"
	recordingDeletePath   = "/v1/audio-assets/:audio_asset_id"
)

type RecordingHTTPService interface {
	Playback(
		context.Context,
		requestcontext.Actor,
		string,
	) (objectstore.SignedGetResult, error)
	Delete(context.Context, requestcontext.Actor, string) error
}

func (handler *Handler) recordingPlayback(c *gin.Context) {
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		handler.write(c, authenticationRequired())
		return
	}
	assetID := c.Param("audio_asset_id")
	if !sharedmedia.ValidUUID(assetID) {
		handler.write(c, invalidRequest(nil))
		return
	}
	result, err := handler.recordings.Playback(
		c.Request.Context(), actor, assetID,
	)
	if err != nil {
		handler.write(c, mapError(err))
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{
		"playback_url": result.URL,
		"expires_at":   result.ExpiresAt,
	})
}

func (handler *Handler) deleteRecording(c *gin.Context) {
	actor, ok := requestcontext.ActorFromContext(c.Request.Context())
	if !ok {
		handler.write(c, authenticationRequired())
		return
	}
	assetID := c.Param("audio_asset_id")
	if !sharedmedia.ValidUUID(assetID) {
		handler.write(c, invalidRequest(nil))
		return
	}
	if err := handler.recordings.Delete(
		c.Request.Context(), actor, assetID,
	); err != nil {
		handler.write(c, mapError(err))
		return
	}
	c.Status(http.StatusNoContent)
}
