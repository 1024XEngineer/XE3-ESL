package media

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"reflect"
	"strings"
	"sync"
	"time"
)

const (
	// MaxAudioBytes keeps a Base64 data URL below Qwen ASR's documented
	// 10 MB encoded-input limit, including the media-type prefix.
	MaxAudioBytes    int64 = 7_400_000
	MaxAudioDuration       = 120 * time.Second

	ContentTypeWAV = "audio/wav"
)

var ErrAudioClosed = errors.New("temporary audio is closed")

// AudioSource exposes only the metadata and fresh reader a provider adapter
// needs. The backing path is deliberately not part of this boundary.
type AudioSource interface {
	Open() (io.ReadCloser, error)
	MediaType() string
	Size() int64
	Duration() time.Duration
	SampleRate() int
}

// ManagedAudioSource makes temporary ownership explicit. Callers that receive
// one must close it after serving or otherwise consuming the audio.
type ManagedAudioSource interface {
	AudioSource
	io.Closer
}

// TemporaryAudio owns a single validated upload. Close removes the backing
// file and is safe to call more than once.
type TemporaryAudio struct {
	mu         sync.RWMutex
	path       string
	mediaType  string
	size       int64
	duration   time.Duration
	sampleRate int
	closed     bool
}

// CaptureTemporaryAudio copies an untrusted upload to a mode-0600 temporary
// file, validates its claimed media type against its bytes, and calculates a
// trustworthy duration. The initial MVP deliberately accepts only 16-bit PCM
// WAV so duration and format checks do not depend on an external decoder.
func CaptureTemporaryAudio(
	directory string,
	claimedContentType string,
	input io.Reader,
) (*TemporaryAudio, error) {
	if isNilReader(input) {
		return nil, errors.New("audio input is required")
	}
	mediaType, err := normalizeContentType(claimedContentType)
	if err != nil {
		return nil, err
	}
	if directory == "" {
		directory = os.TempDir()
	}

	file, err := os.CreateTemp(directory, "xe3-esl-audio-*.wav")
	if err != nil {
		return nil, errors.New("create temporary audio")
	}
	path := file.Name()
	keep := false
	defer func() {
		if !keep {
			_ = file.Close()
			_ = os.Remove(path)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return nil, errors.New("secure temporary audio")
	}

	written, err := io.Copy(file, io.LimitReader(input, MaxAudioBytes+1))
	if err != nil {
		return nil, errors.New("store temporary audio")
	}
	if written == 0 {
		return nil, errors.New("audio input is empty")
	}
	if written > MaxAudioBytes {
		return nil, fmt.Errorf("audio input exceeds %d bytes", MaxAudioBytes)
	}
	if err := file.Close(); err != nil {
		return nil, errors.New("close temporary audio")
	}

	metadata, err := validatePCMWAV(path, written)
	if err != nil {
		return nil, err
	}
	if metadata.duration > MaxAudioDuration {
		return nil, fmt.Errorf("audio duration exceeds %s", MaxAudioDuration)
	}

	keep = true
	return &TemporaryAudio{
		path:       path,
		mediaType:  mediaType,
		size:       written,
		duration:   metadata.duration,
		sampleRate: metadata.sampleRate,
	}, nil
}

func (audio *TemporaryAudio) Open() (io.ReadCloser, error) {
	if audio == nil {
		return nil, ErrAudioClosed
	}
	audio.mu.RLock()
	defer audio.mu.RUnlock()
	if audio.closed {
		return nil, ErrAudioClosed
	}
	file, err := os.Open(audio.path)
	if err != nil {
		return nil, errors.New("open temporary audio")
	}
	return file, nil
}

func (audio *TemporaryAudio) MediaType() string {
	if audio == nil {
		return ""
	}
	return audio.mediaType
}

func (audio *TemporaryAudio) Size() int64 {
	if audio == nil {
		return 0
	}
	return audio.size
}

func (audio *TemporaryAudio) Duration() time.Duration {
	if audio == nil {
		return 0
	}
	return audio.duration
}

func (audio *TemporaryAudio) SampleRate() int {
	if audio == nil {
		return 0
	}
	return audio.sampleRate
}

func (audio *TemporaryAudio) Close() error {
	if audio == nil {
		return nil
	}
	audio.mu.Lock()
	defer audio.mu.Unlock()
	if audio.closed {
		return nil
	}
	if err := os.Remove(audio.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return errors.New("remove temporary audio")
	}
	audio.closed = true
	return nil
}

func ValidateAudioSource(source AudioSource) error {
	if isNilAudioSource(source) {
		return errors.New("audio source is required")
	}
	if source.MediaType() != ContentTypeWAV {
		return errors.New("audio source must be a validated PCM WAV")
	}
	if source.Size() <= 0 || source.Size() > MaxAudioBytes {
		return errors.New("audio source size is outside the accepted range")
	}
	if source.Duration() <= 0 || source.Duration() > MaxAudioDuration {
		return errors.New("audio source duration is outside the accepted range")
	}
	if source.SampleRate() < 8_000 || source.SampleRate() > 48_000 {
		return errors.New("audio source sample rate is outside the accepted range")
	}
	return nil
}

func normalizeContentType(raw string) (string, error) {
	mediaType, parameters, err := mime.ParseMediaType(strings.TrimSpace(raw))
	if err != nil {
		return "", errors.New("audio content type is invalid")
	}
	if len(parameters) != 0 {
		return "", errors.New("audio content type parameters are not accepted")
	}
	switch strings.ToLower(mediaType) {
	case ContentTypeWAV, "audio/x-wav":
		return ContentTypeWAV, nil
	default:
		return "", errors.New("audio content type is not supported")
	}
}

type wavMetadata struct {
	duration   time.Duration
	sampleRate int
}

func validatePCMWAV(path string, size int64) (wavMetadata, error) {
	file, err := os.Open(path)
	if err != nil {
		return wavMetadata{}, errors.New("inspect temporary audio")
	}
	defer file.Close()

	if size < 44 {
		return wavMetadata{}, errors.New("audio bytes are not a complete WAV file")
	}
	header := make([]byte, 12)
	if _, err := io.ReadFull(file, header); err != nil {
		return wavMetadata{}, errors.New("read WAV header")
	}
	if string(header[:4]) != "RIFF" || string(header[8:12]) != "WAVE" {
		return wavMetadata{}, errors.New("audio bytes do not match the claimed WAV type")
	}
	if int64(binary.LittleEndian.Uint32(header[4:8]))+8 != size {
		return wavMetadata{}, errors.New("WAV size declaration does not match the upload")
	}

	var (
		byteRate   uint32
		dataSize   uint32
		sampleRate uint32
		blockAlign uint16
		foundFmt   bool
		foundData  bool
	)
	offset := int64(12)
	for offset+8 <= size {
		chunkHeader := make([]byte, 8)
		if _, err := file.ReadAt(chunkHeader, offset); err != nil {
			return wavMetadata{}, errors.New("read WAV chunk header")
		}
		chunkSize := binary.LittleEndian.Uint32(chunkHeader[4:8])
		chunkStart := offset + 8
		chunkEnd := chunkStart + int64(chunkSize)
		if chunkEnd > size {
			return wavMetadata{}, errors.New("WAV chunk exceeds the upload")
		}

		switch string(chunkHeader[:4]) {
		case "fmt ":
			if foundFmt {
				return wavMetadata{}, errors.New("WAV contains multiple format chunks")
			}
			if chunkSize < 16 {
				return wavMetadata{}, errors.New("WAV format chunk is incomplete")
			}
			format := make([]byte, 16)
			if _, err := file.ReadAt(format, chunkStart); err != nil {
				return wavMetadata{}, errors.New("read WAV format")
			}
			audioFormat := binary.LittleEndian.Uint16(format[0:2])
			channels := binary.LittleEndian.Uint16(format[2:4])
			sampleRate = binary.LittleEndian.Uint32(format[4:8])
			byteRate = binary.LittleEndian.Uint32(format[8:12])
			blockAlign = binary.LittleEndian.Uint16(format[12:14])
			bitsPerSample := binary.LittleEndian.Uint16(format[14:16])
			if audioFormat != 1 ||
				(channels != 1 && channels != 2) ||
				sampleRate < 8_000 ||
				sampleRate > 48_000 ||
				bitsPerSample != 16 {
				return wavMetadata{}, errors.New("WAV must be mono or stereo 16-bit PCM at 8-48 kHz")
			}
			expectedBlockAlign := channels * (bitsPerSample / 8)
			expectedByteRate := sampleRate * uint32(expectedBlockAlign)
			if blockAlign != expectedBlockAlign ||
				byteRate == 0 ||
				byteRate != expectedByteRate {
				return wavMetadata{}, errors.New("WAV format metadata is inconsistent")
			}
			foundFmt = true
		case "data":
			if foundData {
				return wavMetadata{}, errors.New("WAV contains multiple data chunks")
			}
			if chunkSize == 0 {
				return wavMetadata{}, errors.New("WAV audio data is empty")
			}
			dataSize = chunkSize
			foundData = true
		}

		offset = chunkEnd
		if chunkSize%2 != 0 {
			if offset >= size {
				return wavMetadata{}, errors.New("WAV chunk is missing its padding byte")
			}
			offset++
		}
	}
	if offset != size {
		return wavMetadata{}, errors.New("WAV contains trailing or incomplete bytes")
	}
	if !foundFmt || !foundData || byteRate == 0 {
		return wavMetadata{}, errors.New("WAV is missing required format or data chunks")
	}
	if blockAlign == 0 || dataSize%uint32(blockAlign) != 0 {
		return wavMetadata{}, errors.New("WAV audio data does not contain complete PCM frames")
	}
	duration := time.Duration(dataSize) * time.Second / time.Duration(byteRate)
	if duration <= 0 {
		return wavMetadata{}, errors.New("WAV duration is invalid")
	}
	return wavMetadata{
		duration:   duration,
		sampleRate: int(sampleRate),
	}, nil
}

func isNilReader(reader io.Reader) bool {
	if reader == nil {
		return true
	}
	value := reflect.ValueOf(reader)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
		reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func isNilAudioSource(source AudioSource) bool {
	if source == nil {
		return true
	}
	value := reflect.ValueOf(source)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map,
		reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

var _ AudioSource = (*TemporaryAudio)(nil)
var _ ManagedAudioSource = (*TemporaryAudio)(nil)
