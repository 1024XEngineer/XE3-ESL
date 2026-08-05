package meme

import (
	"context"
	"os"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/requestcontext"
)

type Service struct {
	attachments AttachmentReader
	assets      LocalAssetReader
}

func NewService(attachments AttachmentReader, assets LocalAssetReader) (*Service, error) {
	if attachments == nil || assets == nil {
		return nil, ErrInvalidRequest
	}
	return &Service{attachments: attachments, assets: assets}, nil
}

func (service *Service) MessageAttachments(
	ctx context.Context,
	actor requestcontext.Actor,
	threadID string,
	messageID string,
) ([]Attachment, error) {
	if service == nil || !actor.Valid() || threadID == "" || messageID == "" {
		return nil, ErrInvalidRequest
	}
	return service.attachments.MessageAttachments(ctx, actor.UserID, threadID, messageID)
}

func (service *Service) Content(
	ctx context.Context,
	actor requestcontext.Actor,
	attachmentID string,
) (*os.File, Attachment, error) {
	if service == nil || !actor.Valid() || attachmentID == "" {
		return nil, Attachment{}, ErrInvalidRequest
	}
	attachment, err := service.attachments.FindAttachment(ctx, actor.UserID, attachmentID)
	if err != nil {
		return nil, Attachment{}, err
	}
	file, asset, err := service.assets.Open(attachment.AssetKey)
	if err != nil {
		return nil, Attachment{}, err
	}
	if asset.MemeID != attachment.MemeID || asset.ChecksumSHA256 != attachment.ChecksumSHA256 ||
		asset.SizeBytes != attachment.SizeBytes || asset.ContentType != attachment.ContentType {
		_ = file.Close()
		return nil, Attachment{}, ErrRepository
	}
	return file, attachment, nil
}
