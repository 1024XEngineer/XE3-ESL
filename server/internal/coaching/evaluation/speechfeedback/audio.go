package speechfeedback

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/platform/objectstore"
)

var ErrAcousticUnavailable = errors.New("evaluation: acoustic assessment unavailable")

const (
	AudioReadTimeout = 30 * time.Second
	maxAudioBytes    = 9_600_000
	maxSignedURLTTL  = 2 * time.Minute
)

type AudioReader interface {
	ReadOwnedAudio(context.Context, string, string) ([]byte, error)
}

// AudioMetadata is the exact Practice-owned metadata required to assess one
// readable recording. Evaluation never owns or copies the audio lifecycle.
type AudioMetadata struct {
	ObjectKey      string
	Size           int64
	ChecksumSHA256 string
}

type AudioMetadataReader interface {
	GetReadableOwnedAudio(context.Context, string, string) (AudioMetadata, error)
}

type ProductionAudioReader struct {
	metadata AudioMetadataReader
	store    objectstore.Store
	client   *http.Client
}

func NewProductionAudioReader(
	metadata AudioMetadataReader,
	store objectstore.Store,
	client *http.Client,
) (*ProductionAudioReader, error) {
	if metadata == nil || store == nil || client == nil || client.Timeout <= 0 {
		return nil, ErrAcousticUnavailable
	}
	return &ProductionAudioReader{metadata: metadata, store: store, client: client}, nil
}

func NewAudioHTTPClient() *http.Client {
	return &http.Client{
		Timeout: AudioReadTimeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func (reader *ProductionAudioReader) ReadOwnedAudio(
	ctx context.Context,
	userID string,
	audioAssetID string,
) ([]byte, error) {
	if reader == nil || reader.metadata == nil || reader.store == nil ||
		reader.client == nil || ctx == nil || strings.TrimSpace(userID) == "" ||
		strings.TrimSpace(audioAssetID) == "" {
		return nil, ErrAcousticUnavailable
	}
	metadata, err := reader.metadata.GetReadableOwnedAudio(ctx, userID, audioAssetID)
	if err != nil || metadata.ObjectKey == "" || metadata.Size <= 0 ||
		metadata.Size > maxAudioBytes || !validChecksum(metadata.ChecksumSHA256) {
		return nil, ErrAcousticUnavailable
	}
	signed, err := reader.store.SignedGet(ctx, metadata.ObjectKey)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	signedURL, err := url.Parse(signed.URL)
	if err != nil || !strings.EqualFold(signedURL.Scheme, "https") ||
		signedURL.Host == "" || !signed.ExpiresAt.After(now) ||
		signed.ExpiresAt.After(now.Add(maxSignedURLTTL)) {
		return nil, ErrAcousticUnavailable
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, signed.URL, nil)
	if err != nil {
		return nil, ErrAcousticUnavailable
	}
	response, err := reader.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("read Evaluation audio: HTTP %d", response.StatusCode)
	}
	audio, err := io.ReadAll(io.LimitReader(response.Body, metadata.Size+1))
	if err != nil {
		return nil, err
	}
	if int64(len(audio)) != metadata.Size {
		return nil, ErrAcousticUnavailable
	}
	digest := sha256.Sum256(audio)
	if hex.EncodeToString(digest[:]) != metadata.ChecksumSHA256 {
		return nil, ErrAcousticUnavailable
	}
	return audio, nil
}

func validChecksum(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil && value == strings.ToLower(value)
}

func pcm16Mono(wav []byte) ([]byte, error) {
	if len(wav) < 12 || string(wav[:4]) != "RIFF" || string(wav[8:12]) != "WAVE" {
		return nil, ErrAcousticUnavailable
	}
	var formatFound bool
	var pcm []byte
	for offset := 12; offset+8 <= len(wav); {
		chunkSize := int(binary.LittleEndian.Uint32(wav[offset+4 : offset+8]))
		start := offset + 8
		end := start + chunkSize
		if end > len(wav) {
			return nil, ErrAcousticUnavailable
		}
		switch string(wav[offset : offset+4]) {
		case "fmt ":
			if formatFound || chunkSize < 16 ||
				binary.LittleEndian.Uint16(wav[start:start+2]) != 1 ||
				binary.LittleEndian.Uint16(wav[start+2:start+4]) != 1 ||
				binary.LittleEndian.Uint32(wav[start+4:start+8]) != 16_000 ||
				binary.LittleEndian.Uint16(wav[start+14:start+16]) != 16 {
				return nil, ErrAcousticUnavailable
			}
			formatFound = true
		case "data":
			if pcm != nil || chunkSize == 0 {
				return nil, ErrAcousticUnavailable
			}
			pcm = wav[start:end]
		}
		offset = end + chunkSize%2
	}
	if !formatFound || len(pcm) == 0 || len(pcm)%2 != 0 {
		return nil, ErrAcousticUnavailable
	}
	return pcm, nil
}

var _ AudioReader = (*ProductionAudioReader)(nil)
