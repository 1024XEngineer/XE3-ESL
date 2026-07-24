package media

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCaptureTemporaryAudioValidatesAndRemovesPCMWAV(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	payload := testWAV(t, time.Second)
	audio, err := CaptureTemporaryAudio(
		directory,
		"audio/x-wav",
		bytes.NewReader(payload),
	)
	if err != nil {
		t.Fatalf("capture audio: %v", err)
	}
	if audio.MediaType() != ContentTypeWAV ||
		audio.Size() != int64(len(payload)) ||
		audio.Duration() != time.Second ||
		audio.SampleRate() != 8_000 {
		t.Fatalf("unexpected audio metadata: %#v", audio)
	}
	info, err := os.Stat(audio.path)
	if err != nil {
		t.Fatalf("stat temporary audio: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("temporary audio permissions = %o, want 600", info.Mode().Perm())
	}
	reader, err := audio.Open()
	if err != nil {
		t.Fatalf("open audio: %v", err)
	}
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read audio: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close reader: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("temporary audio bytes changed")
	}
	if err := ValidateAudioSource(audio); err != nil {
		t.Fatalf("validate audio source: %v", err)
	}

	if err := audio.Close(); err != nil {
		t.Fatalf("remove audio: %v", err)
	}
	if err := audio.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	if _, err := audio.Open(); !errors.Is(err, ErrAudioClosed) {
		t.Fatalf("open after close = %v, want ErrAudioClosed", err)
	}
	if _, err := os.Stat(audio.path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary audio remained after close: %v", err)
	}
}

func TestCaptureTemporaryAudioRejectsUntrustedInputAndCleansUp(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		contentType string
		payload     []byte
	}{
		{name: "empty", contentType: ContentTypeWAV},
		{name: "unsupported type", contentType: "audio/mpeg", payload: []byte("ID3")},
		{
			name:        "type parameters",
			contentType: "audio/wav; codecs=1",
			payload:     testWAV(t, time.Second),
		},
		{
			name:        "forged WAV",
			contentType: ContentTypeWAV,
			payload:     bytes.Repeat([]byte("not-wav"), 10),
		},
		{
			name:        "partial PCM frame",
			contentType: ContentTypeWAV,
			payload:     incompletePCMFrameWAV(),
		},
		{
			name:        "over duration",
			contentType: ContentTypeWAV,
			payload:     testWAV(t, MaxAudioDuration+time.Second),
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			directory := t.TempDir()
			if _, err := CaptureTemporaryAudio(
				directory,
				test.contentType,
				bytes.NewReader(test.payload),
			); err == nil {
				t.Fatal("expected capture error")
			}
			entries, err := os.ReadDir(directory)
			if err != nil {
				t.Fatalf("read temporary directory: %v", err)
			}
			if len(entries) != 0 {
				t.Fatalf("failed capture left temporary files: %v", entries)
			}
		})
	}
}

func incompletePCMFrameWAV() []byte {
	payload := make([]byte, 46)
	copy(payload[0:4], "RIFF")
	binary.LittleEndian.PutUint32(payload[4:8], uint32(len(payload)-8))
	copy(payload[8:12], "WAVE")
	copy(payload[12:16], "fmt ")
	binary.LittleEndian.PutUint32(payload[16:20], 16)
	binary.LittleEndian.PutUint16(payload[20:22], 1)
	binary.LittleEndian.PutUint16(payload[22:24], 1)
	binary.LittleEndian.PutUint32(payload[24:28], 8_000)
	binary.LittleEndian.PutUint32(payload[28:32], 16_000)
	binary.LittleEndian.PutUint16(payload[32:34], 2)
	binary.LittleEndian.PutUint16(payload[34:36], 16)
	copy(payload[36:40], "data")
	binary.LittleEndian.PutUint32(payload[40:44], 1)
	payload[44] = 1
	payload[45] = 0 // RIFF padding for the odd-sized data chunk.
	return payload
}

func TestCaptureTemporaryAudioRejectsOversizedStreamWithoutRetainingIt(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	// A synthetic reader avoids allocating the full upload in the test.
	reader := io.MultiReader(
		io.LimitReader(zeroReader{}, MaxAudioBytes),
		strings.NewReader("x"),
	)
	if _, err := CaptureTemporaryAudio(
		directory,
		ContentTypeWAV,
		reader,
	); err == nil {
		t.Fatal("expected oversized audio error")
	}
	matches, err := filepath.Glob(filepath.Join(directory, "xe3-esl-audio-*"))
	if err != nil {
		t.Fatalf("glob temporary audio: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("oversized upload left files: %v", matches)
	}
}

func TestValidateAudioSourceRejectsInvalidMetadata(t *testing.T) {
	t.Parallel()

	var typedNil *TemporaryAudio
	tests := map[string]AudioSource{
		"typed nil":       typedNil,
		"wrong type":      stubAudio{mediaType: "audio/mpeg", size: 10, duration: time.Second, sampleRate: 16_000},
		"empty":           stubAudio{mediaType: ContentTypeWAV, duration: time.Second, sampleRate: 16_000},
		"oversized":       stubAudio{mediaType: ContentTypeWAV, size: MaxAudioBytes + 1, duration: time.Second, sampleRate: 16_000},
		"zero duration":   stubAudio{mediaType: ContentTypeWAV, size: 10, sampleRate: 16_000},
		"long duration":   stubAudio{mediaType: ContentTypeWAV, size: 10, duration: MaxAudioDuration + 1, sampleRate: 16_000},
		"bad sample rate": stubAudio{mediaType: ContentTypeWAV, size: 10, duration: time.Second, sampleRate: 96_000},
	}
	for name, source := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := ValidateAudioSource(source); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func testWAV(t *testing.T, duration time.Duration) []byte {
	t.Helper()
	const (
		channels      = 1
		sampleRate    = 8_000
		bitsPerSample = 16
	)
	byteRate := sampleRate * channels * bitsPerSample / 8
	dataSize := int64(duration) * int64(byteRate) / int64(time.Second)
	if dataSize > int64(^uint32(0)) {
		t.Fatal("test WAV is too large")
	}
	payload := make([]byte, 44+dataSize)
	copy(payload[0:4], "RIFF")
	binary.LittleEndian.PutUint32(payload[4:8], uint32(len(payload)-8))
	copy(payload[8:12], "WAVE")
	copy(payload[12:16], "fmt ")
	binary.LittleEndian.PutUint32(payload[16:20], 16)
	binary.LittleEndian.PutUint16(payload[20:22], 1)
	binary.LittleEndian.PutUint16(payload[22:24], channels)
	binary.LittleEndian.PutUint32(payload[24:28], sampleRate)
	binary.LittleEndian.PutUint32(payload[28:32], uint32(byteRate))
	binary.LittleEndian.PutUint16(payload[32:34], channels*bitsPerSample/8)
	binary.LittleEndian.PutUint16(payload[34:36], bitsPerSample)
	copy(payload[36:40], "data")
	binary.LittleEndian.PutUint32(payload[40:44], uint32(dataSize))
	return payload
}

type stubAudio struct {
	mediaType  string
	size       int64
	duration   time.Duration
	sampleRate int
}

func (audio stubAudio) Open() (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("audio")), nil
}

func (audio stubAudio) MediaType() string       { return audio.mediaType }
func (audio stubAudio) Size() int64             { return audio.size }
func (audio stubAudio) Duration() time.Duration { return audio.duration }
func (audio stubAudio) SampleRate() int         { return audio.sampleRate }

type zeroReader struct{}

func (zeroReader) Read(buffer []byte) (int, error) {
	for index := range buffer {
		buffer[index] = 0
	}
	return len(buffer), nil
}
