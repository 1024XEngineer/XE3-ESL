package qianwen

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/1024XEngineer/XE3-ESL/server/internal/ai"
	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
)

const testTTSProviderURL = "http://dashscope-result-bj.oss-cn-beijing.aliyuncs.com/" +
	"safe/audio.wav?Expires=4102444800&Signature=test"
const testTTSDownloadURL = "https://dashscope-result-bj.oss-cn-beijing.aliyuncs.com/" +
	"safe/audio.wav?Expires=4102444800&Signature=test"

func TestNormalizeProviderWAVSizeMarkers(t *testing.T) {
	t.Parallel()

	if providerRIFFSizeMarker != 0x7fffffbf {
		t.Fatalf("RIFF size marker = %#x", providerRIFFSizeMarker)
	}
	if providerDataSizeMarker != 0x7fffff9b {
		t.Fatalf("data size marker = %#x", providerDataSizeMarker)
	}
	wav := ttsProviderMarkerWAV(100 * time.Millisecond)
	original := append([]byte(nil), wav...)

	reader, err := normalizeProviderWAVSizeMarkers(
		bytes.NewReader(wav),
		int64(len(wav)),
	)
	if err != nil {
		t.Fatalf("normalize provider WAV: %v", err)
	}
	normalized, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read normalized provider WAV: %v", err)
	}
	if len(normalized) != len(wav) ||
		binary.LittleEndian.Uint32(normalized[4:8]) != uint32(len(wav)-8) ||
		binary.LittleEndian.Uint32(normalized[40:44]) != uint32(len(wav)-44) {
		t.Fatalf(
			"unexpected normalized WAV sizes: bytes=%d riff=%d data=%d",
			len(normalized),
			binary.LittleEndian.Uint32(normalized[4:8]),
			binary.LittleEndian.Uint32(normalized[40:44]),
		)
	}
	for index := range normalized {
		if (index >= 4 && index < 8) || (index >= 40 && index < 44) {
			continue
		}
		if normalized[index] != original[index] {
			t.Fatalf("normalized WAV changed payload byte %d", index)
		}
	}
}

func TestNormalizeProviderWAVSizeMarkersLeavesOtherInputsUnchanged(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		contentLength func([]byte) int64
		mutate        func([]byte)
	}{
		{
			name:          "normal WAV",
			contentLength: func(wav []byte) int64 { return int64(len(wav)) },
		},
		{
			name:          "unknown content length",
			contentLength: func([]byte) int64 { return -1 },
			mutate:        setProviderWAVSizeMarkers,
		},
		{
			name:          "zero content length",
			contentLength: func([]byte) int64 { return 0 },
			mutate:        setProviderWAVSizeMarkers,
		},
		{
			name: "over-limit content length",
			contentLength: func([]byte) int64 {
				return platformmedia.MaxAudioBytes + 1
			},
			mutate: setProviderWAVSizeMarkers,
		},
		{
			name:          "only RIFF marker",
			contentLength: func(wav []byte) int64 { return int64(len(wav)) },
			mutate: func(wav []byte) {
				binary.LittleEndian.PutUint32(wav[4:8], providerRIFFSizeMarker)
			},
		},
		{
			name:          "only data marker",
			contentLength: func(wav []byte) int64 { return int64(len(wav)) },
			mutate: func(wav []byte) {
				binary.LittleEndian.PutUint32(wav[40:44], providerDataSizeMarker)
			},
		},
		{
			name:          "unknown markers",
			contentLength: func(wav []byte) int64 { return int64(len(wav)) },
			mutate: func(wav []byte) {
				binary.LittleEndian.PutUint32(wav[4:8], 0x7fffffbe)
				binary.LittleEndian.PutUint32(wav[40:44], 0x7fffff9a)
			},
		},
		{
			name:          "non-standard 44-byte header",
			contentLength: func(wav []byte) int64 { return int64(len(wav)) },
			mutate: func(wav []byte) {
				setProviderWAVSizeMarkers(wav)
				binary.LittleEndian.PutUint32(wav[16:20], 18)
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			wav := ttsTestWAV(100 * time.Millisecond)
			if test.mutate != nil {
				test.mutate(wav)
			}
			expected := append([]byte(nil), wav...)
			reader, err := normalizeProviderWAVSizeMarkers(
				bytes.NewReader(wav),
				test.contentLength(wav),
			)
			if err != nil {
				t.Fatalf("normalize provider WAV: %v", err)
			}
			got, err := io.ReadAll(reader)
			if err != nil {
				t.Fatalf("read provider WAV: %v", err)
			}
			if !bytes.Equal(got, expected) {
				t.Fatal("provider WAV changed outside the exact marker contract")
			}
		})
	}
}

func TestNormalizeProviderWAVSizeMarkersRejectsShortRead(t *testing.T) {
	t.Parallel()

	wav := ttsProviderMarkerWAV(100 * time.Millisecond)
	if _, err := normalizeProviderWAVSizeMarkers(
		bytes.NewReader(wav[:40]),
		44,
	); err == nil {
		t.Fatal("expected incomplete provider WAV header error")
	}
}

func TestSynthesizeAcceptsExactProviderWAVSizeMarkers(t *testing.T) {
	t.Parallel()

	wav := ttsProviderMarkerWAV(100 * time.Millisecond)
	directory := t.TempDir()
	var calls atomic.Int32
	synthesizer := mustSynthesizer(
		t,
		doerFunc(func(*http.Request) (*http.Response, error) {
			if calls.Add(1) == 1 {
				return jsonResponse(
					http.StatusOK,
					successfulTTSResponse(testTTSProviderURL),
				), nil
			}
			response := wavResponse(wav, platformmedia.ContentTypeWAV)
			response.Header.Set("Content-Encoding", "identity")
			return response, nil
		}),
		"test-api-key",
		directory,
	)
	result, err := synthesizer.Synthesize(
		context.Background(),
		ai.SynthesisRequest{Text: "Question"},
	)
	if err != nil {
		t.Fatalf("synthesize provider marker WAV: %v", err)
	}
	if result.Audio == nil ||
		result.Audio.Size() != int64(len(wav)) ||
		result.Audio.SampleRate() != ttsOutputSampleRate {
		t.Fatalf("unexpected normalized provider audio: %#v", result.Audio)
	}
	if err := result.Audio.Close(); err != nil {
		t.Fatalf("close normalized provider audio: %v", err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read temp directory: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("closed provider WAV left temporary files: %v", entries)
	}
}

func TestSynthesizeRejectsUnsafeProviderWAVVariantsAndCleansUp(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		body          func() []byte
		contentLength func([]byte) int64
		mutate        func([]byte)
	}{
		{
			name: "unknown content length",
			body: func() []byte {
				return ttsProviderMarkerWAV(100 * time.Millisecond)
			},
			contentLength: func([]byte) int64 { return -1 },
		},
		{
			name: "zero content length",
			body: func() []byte {
				return ttsProviderMarkerWAV(100 * time.Millisecond)
			},
			contentLength: func([]byte) int64 { return 0 },
		},
		{
			name: "over-limit content length",
			body: func() []byte {
				return ttsProviderMarkerWAV(100 * time.Millisecond)
			},
			contentLength: func([]byte) int64 {
				return platformmedia.MaxAudioBytes + 1
			},
		},
		{
			name: "short read",
			body: func() []byte {
				return ttsProviderMarkerWAV(100 * time.Millisecond)[:40]
			},
			contentLength: func([]byte) int64 { return 44 },
		},
		{
			name: "long read",
			body: func() []byte {
				return ttsProviderMarkerWAV(100 * time.Millisecond)
			},
			contentLength: func(wav []byte) int64 {
				return int64(len(wav) - 2)
			},
		},
		{
			name: "only RIFF marker",
			body: func() []byte {
				return ttsTestWAV(100 * time.Millisecond)
			},
			mutate: func(wav []byte) {
				binary.LittleEndian.PutUint32(wav[4:8], providerRIFFSizeMarker)
			},
		},
		{
			name: "only data marker",
			body: func() []byte {
				return ttsTestWAV(100 * time.Millisecond)
			},
			mutate: func(wav []byte) {
				binary.LittleEndian.PutUint32(wav[40:44], providerDataSizeMarker)
			},
		},
		{
			name: "unknown markers",
			body: func() []byte {
				return ttsTestWAV(100 * time.Millisecond)
			},
			mutate: func(wav []byte) {
				binary.LittleEndian.PutUint32(wav[4:8], 0x7fffffbe)
				binary.LittleEndian.PutUint32(wav[40:44], 0x7fffff9a)
			},
		},
		{
			name: "non-standard 44-byte header",
			body: func() []byte {
				return ttsProviderMarkerWAV(100 * time.Millisecond)
			},
			mutate: func(wav []byte) {
				binary.LittleEndian.PutUint32(wav[16:20], 18)
			},
		},
		{
			name: "non-PCM",
			body: func() []byte {
				return ttsProviderMarkerWAV(100 * time.Millisecond)
			},
			mutate: func(wav []byte) {
				binary.LittleEndian.PutUint16(wav[20:22], 3)
			},
		},
		{
			name: "stereo",
			body: func() []byte {
				return ttsProviderMarkerWAV(100 * time.Millisecond)
			},
			mutate: func(wav []byte) {
				binary.LittleEndian.PutUint16(wav[22:24], 2)
				binary.LittleEndian.PutUint32(wav[28:32], 96_000)
				binary.LittleEndian.PutUint16(wav[32:34], 4)
			},
		},
		{
			name: "wrong byte rate",
			body: func() []byte {
				return ttsProviderMarkerWAV(100 * time.Millisecond)
			},
			mutate: func(wav []byte) {
				binary.LittleEndian.PutUint32(wav[28:32], 47_998)
			},
		},
		{
			name: "non-16-bit",
			body: func() []byte {
				return ttsProviderMarkerWAV(100 * time.Millisecond)
			},
			mutate: func(wav []byte) {
				binary.LittleEndian.PutUint16(wav[34:36], 24)
			},
		},
		{
			name: "non-24-kHz",
			body: func() []byte {
				return ttsProviderMarkerWAV(100 * time.Millisecond)
			},
			mutate: func(wav []byte) {
				binary.LittleEndian.PutUint32(wav[24:28], 16_000)
				binary.LittleEndian.PutUint32(wav[28:32], 32_000)
			},
		},
		{
			name: "wrong block align",
			body: func() []byte {
				return ttsProviderMarkerWAV(100 * time.Millisecond)
			},
			mutate: func(wav []byte) {
				binary.LittleEndian.PutUint16(wav[32:34], 4)
			},
		},
		{
			name: "data is not a complete PCM frame",
			body: func() []byte {
				return append(
					ttsProviderMarkerWAV(100*time.Millisecond),
					0,
					0,
				)
			},
			mutate: func(wav []byte) {
				binary.LittleEndian.PutUint16(wav[22:24], 2)
				binary.LittleEndian.PutUint32(wav[28:32], 96_000)
				binary.LittleEndian.PutUint16(wav[32:34], 4)
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			wav := test.body()
			if test.mutate != nil {
				test.mutate(wav)
			}
			contentLength := int64(len(wav))
			if test.contentLength != nil {
				contentLength = test.contentLength(wav)
			}
			directory := t.TempDir()
			var calls atomic.Int32
			synthesizer := mustSynthesizer(
				t,
				doerFunc(func(*http.Request) (*http.Response, error) {
					if calls.Add(1) == 1 {
						return jsonResponse(
							http.StatusOK,
							successfulTTSResponse(testTTSProviderURL),
						), nil
					}
					response := wavResponse(wav, platformmedia.ContentTypeWAV)
					response.ContentLength = contentLength
					return response, nil
				}),
				"test-api-key",
				directory,
			)
			_, err := synthesizer.Synthesize(
				context.Background(),
				ai.SynthesisRequest{Text: "Question"},
			)
			assertSpeechError(
				t,
				err,
				ai.SpeechOperationSynthesis,
				ai.ErrorInvalidResponse,
				true,
			)
			entries, readErr := os.ReadDir(directory)
			if readErr != nil {
				t.Fatalf("read temp directory: %v", readErr)
			}
			if len(entries) != 0 {
				t.Fatalf("failed provider WAV left temporary files: %v", entries)
			}
		})
	}
}

func TestGlobalMediaBoundaryStillRejectsProviderWAVSizeMarkers(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	audio, err := platformmedia.CaptureTemporaryAudio(
		directory,
		platformmedia.ContentTypeWAV,
		bytes.NewReader(ttsProviderMarkerWAV(100*time.Millisecond)),
	)
	if audio != nil {
		_ = audio.Close()
		t.Fatal("global media boundary accepted provider WAV size markers")
	}
	if err == nil {
		t.Fatal("expected global media boundary rejection")
	}
	entries, readErr := os.ReadDir(directory)
	if readErr != nil {
		t.Fatalf("read temp directory: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("global rejection left temporary files: %v", entries)
	}
}

func TestSynthesizeUsesDocumentedContractAndOwnsDownloadedAudio(t *testing.T) {
	t.Parallel()

	const apiKey = "test-api-key"
	wav := ttsTestWAV(100 * time.Millisecond)
	var received ttsRequest
	var calls atomic.Int32
	client := doerFunc(func(request *http.Request) (*http.Response, error) {
		switch calls.Add(1) {
		case 1:
			if request.Method != http.MethodPost ||
				request.URL.String() != "https://dashscope.aliyuncs.com/api/v1/services/audio/tts/SpeechSynthesizer" {
				t.Fatalf("unexpected TTS request: %s %s", request.Method, request.URL)
			}
			if request.Header.Get(authorizationHeaderName) != "Bearer "+apiKey {
				t.Fatal("TTS authorization header was not set")
			}
			if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
				t.Fatalf("decode TTS request: %v", err)
			}
			return jsonResponse(http.StatusOK, successfulTTSResponse(testTTSProviderURL)), nil
		case 2:
			if request.Method != http.MethodGet || request.URL.String() != testTTSDownloadURL {
				t.Fatalf("unexpected audio download: %s %s", request.Method, request.URL)
			}
			if request.Header.Get(authorizationHeaderName) != "" {
				t.Fatal("API key was forwarded to the signed audio URL")
			}
			return wavResponse(wav, platformmedia.ContentTypeWAV), nil
		default:
			t.Fatalf("unexpected HTTP call %d", calls.Load())
			return nil, nil
		}
	})
	synthesizer := mustSynthesizer(t, client, apiKey, t.TempDir())

	result, err := synthesizer.Synthesize(
		context.Background(),
		ai.SynthesisRequest{Text: "  Repeat after me.  "},
	)
	if err != nil {
		t.Fatalf("synthesize: %v", err)
	}
	defer result.Audio.Close()
	if received.Model != "qwen-audio-3.0-tts-flash" ||
		received.Input.Text != "Repeat after me." ||
		received.Input.Voice != "loongeva_v3.6" ||
		received.Input.Format != "wav" ||
		received.Input.SampleRate != 24_000 ||
		len(received.Input.LanguageHints) != 1 ||
		received.Input.LanguageHints[0] != "en" {
		t.Fatalf("unexpected TTS payload: %#v", received)
	}
	if result.RequestID != "tts-request-safe-1" ||
		result.Provider != providerName ||
		result.Model != "qwen-audio-3.0-tts-flash" ||
		result.AudioID != "audio_safe_1" ||
		result.Audio == nil ||
		result.Usage.Characters != 16 {
		t.Fatalf("unexpected TTS result: %#v", result)
	}
	if result.Audio.MediaType() != platformmedia.ContentTypeWAV ||
		result.Audio.Duration() != 100*time.Millisecond ||
		result.Audio.SampleRate() != 24_000 {
		t.Fatalf("unexpected downloaded audio metadata: %#v", result.Audio)
	}
	reader, err := result.Audio.Open()
	if err != nil {
		t.Fatalf("open downloaded audio: %v", err)
	}
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read downloaded audio: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close downloaded audio reader: %v", err)
	}
	if !bytes.Equal(got, wav) {
		t.Fatal("downloaded audio bytes changed")
	}
	if calls.Load() != 2 {
		t.Fatalf("HTTP calls = %d, want 2", calls.Load())
	}
}

func TestSynthesizeRejectsUnsafeProviderURLWithoutDownloading(t *testing.T) {
	t.Parallel()

	urls := []string{
		"ftp://dashscope-result-bj.oss-cn-beijing.aliyuncs.com/audio.wav",
		"https://example.com/audio.wav",
		"https://user:password@dashscope-result-bj.oss-cn-beijing.aliyuncs.com/audio.wav",
		"https://dashscope-result-bj.oss-cn-beijing.aliyuncs.com:443/audio.wav",
	}
	for _, rawURL := range urls {
		rawURL := rawURL
		t.Run(rawURL, func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int32
			synthesizer := mustSynthesizer(t, doerFunc(func(*http.Request) (*http.Response, error) {
				calls.Add(1)
				return jsonResponse(http.StatusOK, successfulTTSResponse(rawURL)), nil
			}), "test-api-key", t.TempDir())

			_, err := synthesizer.Synthesize(
				context.Background(),
				ai.SynthesisRequest{Text: "Question"},
			)
			assertSpeechError(
				t,
				err,
				ai.SpeechOperationSynthesis,
				ai.ErrorInvalidResponse,
				true,
			)
			if calls.Load() != 1 {
				t.Fatalf("HTTP calls = %d, unsafe URL must not be fetched", calls.Load())
			}
		})
	}
}

func TestSynthesizeRejectsEncodedProviderAudioAndCleansUp(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		uncompressed bool
		encoding     string
	}{
		{name: "automatic decompression", uncompressed: true},
		{name: "gzip content encoding", encoding: "gzip"},
		{name: "multiple content encodings", encoding: "gzip, br"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			directory := t.TempDir()
			wav := ttsTestWAV(100 * time.Millisecond)
			var calls atomic.Int32
			synthesizer := mustSynthesizer(
				t,
				doerFunc(func(*http.Request) (*http.Response, error) {
					if calls.Add(1) == 1 {
						return jsonResponse(
							http.StatusOK,
							successfulTTSResponse(testTTSProviderURL),
						), nil
					}
					response := wavResponse(
						wav,
						platformmedia.ContentTypeWAV,
					)
					response.Uncompressed = test.uncompressed
					if test.encoding != "" {
						response.Header.Set(
							"Content-Encoding",
							test.encoding,
						)
					}
					return response, nil
				}),
				"test-api-key",
				directory,
			)
			_, err := synthesizer.Synthesize(
				context.Background(),
				ai.SynthesisRequest{Text: "Question"},
			)
			assertSpeechError(
				t,
				err,
				ai.SpeechOperationSynthesis,
				ai.ErrorInvalidResponse,
				true,
			)
			entries, readErr := os.ReadDir(directory)
			if readErr != nil {
				t.Fatalf("read temp directory: %v", readErr)
			}
			if len(entries) != 0 {
				t.Fatalf("encoded response left temporary files: %v", entries)
			}
		})
	}
}

func TestSynthesizeRejectsUntrustedDownloadedMediaAndCleansUp(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		contentType string
		body        []byte
	}{
		{
			name:        "forged WAV",
			contentType: platformmedia.ContentTypeWAV,
			body:        []byte("not a WAV"),
		},
		{
			name:        "wrong content type",
			contentType: "text/html",
			body:        []byte("<html>private error</html>"),
		},
		{
			name:        "oversized body",
			contentType: platformmedia.ContentTypeWAV,
			body:        bytes.Repeat([]byte{0}, int(platformmedia.MaxAudioBytes)+1),
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			directory := t.TempDir()
			var calls atomic.Int32
			synthesizer := mustSynthesizer(t, doerFunc(func(*http.Request) (*http.Response, error) {
				if calls.Add(1) == 1 {
					return jsonResponse(
						http.StatusOK,
						successfulTTSResponse(testTTSProviderURL),
					), nil
				}
				return wavResponse(test.body, test.contentType), nil
			}), "test-api-key", directory)
			_, err := synthesizer.Synthesize(
				context.Background(),
				ai.SynthesisRequest{Text: "Question"},
			)
			assertSpeechError(
				t,
				err,
				ai.SpeechOperationSynthesis,
				ai.ErrorInvalidResponse,
				true,
			)
			entries, readErr := os.ReadDir(directory)
			if readErr != nil {
				t.Fatalf("read temp directory: %v", readErr)
			}
			if len(entries) != 0 {
				t.Fatalf("failed TTS download left temporary files: %v", entries)
			}
		})
	}
}

func TestSynthesizeRejectsInvalidRequestBeforeProviderCall(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	synthesizer := mustSynthesizer(t, doerFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return jsonResponse(http.StatusOK, `{}`), nil
	}), "test-api-key", t.TempDir())
	_, err := synthesizer.Synthesize(context.Background(), ai.SynthesisRequest{})
	assertSpeechError(t, err, ai.SpeechOperationSynthesis, ai.ErrorInvalidRequest, false)
	if calls.Load() != 0 {
		t.Fatalf("provider calls = %d, want zero", calls.Load())
	}
}

func TestSynthesizeMapsProviderErrorWithoutLeakingTextOrCredentials(t *testing.T) {
	t.Parallel()

	const sensitive = "private-tts-secret"
	synthesizer := mustSynthesizer(t, doerFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(
			http.StatusTooManyRequests,
			`{"code":"Throttling.RateQuota","message":"private-tts-secret"}`,
		), nil
	}), sensitive, t.TempDir())
	_, err := synthesizer.Synthesize(
		context.Background(),
		ai.SynthesisRequest{Text: sensitive},
	)
	assertSpeechError(
		t,
		err,
		ai.SpeechOperationSynthesis,
		ai.ErrorRateLimited,
		true,
	)
	if strings.Contains(err.Error(), sensitive) {
		t.Fatalf("TTS error leaked sensitive value: %q", err)
	}
}

func TestSynthesizerTimeoutRedirectPolicyAndFormattingAreSafe(t *testing.T) {
	t.Parallel()

	const apiKey = "must-never-be-logged"
	synthesizer, err := NewSynthesizer(TTSConfig{
		BaseURL:      "https://dashscope.aliyuncs.com/api/v1",
		Model:        "qwen-audio-3.0-tts-flash",
		Voice:        "loongeva_v3.6",
		LanguageHint: "en",
		Timeout:      time.Second,
	}, apiKey)
	if err != nil {
		t.Fatalf("new synthesizer: %v", err)
	}
	client, ok := synthesizer.client.(*http.Client)
	if !ok {
		t.Fatalf("client has unexpected type %T", synthesizer.client)
	}
	if err := client.CheckRedirect(&http.Request{}, nil); err != http.ErrUseLastResponse {
		t.Fatalf("redirect policy returned %v", err)
	}
	for _, value := range []string{
		fmt.Sprint(synthesizer),
		fmt.Sprintf("%+v", synthesizer),
		fmt.Sprintf("%#v", synthesizer),
		fmt.Sprint(*synthesizer),
		fmt.Sprintf("%+v", *synthesizer),
		fmt.Sprintf("%#v", *synthesizer),
	} {
		if strings.Contains(value, apiKey) {
			t.Fatalf("synthesizer formatting exposed API key: %q", value)
		}
	}
	var nilSynthesizer *Synthesizer
	for _, value := range []string{
		fmt.Sprint(nilSynthesizer),
		fmt.Sprintf("%+v", nilSynthesizer),
		fmt.Sprintf("%#v", nilSynthesizer),
	} {
		if strings.Contains(value, apiKey) {
			t.Fatalf("nil synthesizer formatting exposed API key: %q", value)
		}
	}
}

func TestNewSynthesizerRejectsUnsupportedConfiguration(t *testing.T) {
	t.Parallel()

	valid := TTSConfig{
		BaseURL:      "https://dashscope.aliyuncs.com/api/v1",
		Model:        "qwen-audio-3.0-tts-flash",
		Voice:        "loongeva_v3.6",
		LanguageHint: "en",
		Timeout:      time.Second,
	}
	tests := []struct {
		name   string
		mutate func(*TTSConfig)
	}{
		{name: "unsupported model API family", mutate: func(config *TTSConfig) {
			config.Model = "qwen3-tts-flash"
		}},
		{name: "wrong voice", mutate: func(config *TTSConfig) {
			config.Voice = "loongeva_v3.6\ninjected"
		}},
		{name: "wrong language", mutate: func(config *TTSConfig) {
			config.LanguageHint = "Auto"
		}},
		{name: "wrong base path", mutate: func(config *TTSConfig) {
			config.BaseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
		}},
		{name: "Singapore legacy endpoint", mutate: func(config *TTSConfig) {
			config.BaseURL = "https://dashscope-intl.aliyuncs.com/api/v1"
		}},
		{name: "Singapore workspace endpoint", mutate: func(config *TTSConfig) {
			config.BaseURL = "https://workspace.ap-southeast-1.maas.aliyuncs.com/api/v1"
		}},
		{name: "US endpoint", mutate: func(config *TTSConfig) {
			config.BaseURL = "https://dashscope-us.aliyuncs.com/api/v1"
		}},
		{name: "zero timeout", mutate: func(config *TTSConfig) {
			config.Timeout = 0
		}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config := valid
			test.mutate(&config)
			if _, err := NewSynthesizer(config, "test-api-key"); err == nil {
				t.Fatal("expected configuration error")
			}
		})
	}
}

func TestNewSynthesizerAcceptsBeijingWorkspaceEndpoint(t *testing.T) {
	t.Parallel()

	synthesizer, err := newSynthesizerWithClient(TTSConfig{
		BaseURL:      "https://workspace-123.cn-beijing.maas.aliyuncs.com/api/v1",
		Model:        "qwen-audio-3.0-tts-flash",
		Voice:        "loongeva_v3.6",
		LanguageHint: "en",
		Timeout:      time.Second,
	}, "test-api-key", doerFunc(func(*http.Request) (*http.Response, error) {
		return nil, nil
	}))
	if err != nil {
		t.Fatalf("new synthesizer: %v", err)
	}
	if synthesizer.endpoint != "https://workspace-123.cn-beijing.maas.aliyuncs.com"+
		"/api/v1/services/audio/tts/SpeechSynthesizer" {
		t.Fatalf("unexpected Beijing endpoint: %s", synthesizer.endpoint)
	}
}

func mustSynthesizer(
	t *testing.T,
	client httpDoer,
	apiKey string,
	tempDirectory string,
) *Synthesizer {
	t.Helper()
	synthesizer, err := newSynthesizerWithClient(TTSConfig{
		BaseURL:       "https://dashscope.aliyuncs.com/api/v1",
		Model:         "qwen-audio-3.0-tts-flash",
		Voice:         "loongeva_v3.6",
		LanguageHint:  "en",
		Timeout:       time.Second,
		TempDirectory: tempDirectory,
	}, apiKey, client)
	if err != nil {
		t.Fatalf("new synthesizer: %v", err)
	}
	synthesizer.now = func() time.Time {
		return time.Unix(2_000_000_000, 0)
	}
	return synthesizer
}

func successfulTTSResponse(audioURL string) string {
	return `{
		"request_id":"tts-request-safe-1",
		"message":"",
		"output":{
			"finish_reason":"stop",
			"audio":{
				"data":"",
				"url":` + quotedJSON(audioURL) + `,
				"id":"audio_safe_1",
				"expires_at":4102444800
			}
		},
		"usage":{
			"input_tokens":4,
			"output_tokens":20,
			"total_tokens":24,
			"characters":16
		}
	}`
}

func quotedJSON(value string) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func wavResponse(body []byte, contentType string) *http.Response {
	return &http.Response{
		StatusCode:    http.StatusOK,
		Header:        http.Header{"Content-Type": []string{contentType}},
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
	}
}

func ttsTestWAV(duration time.Duration) []byte {
	const (
		channels      = 1
		sampleRate    = 24_000
		bitsPerSample = 16
	)
	byteRate := sampleRate * channels * bitsPerSample / 8
	dataSize := int64(duration) * int64(byteRate) / int64(time.Second)
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

func ttsProviderMarkerWAV(duration time.Duration) []byte {
	wav := ttsTestWAV(duration)
	setProviderWAVSizeMarkers(wav)
	return wav
}

func setProviderWAVSizeMarkers(wav []byte) {
	binary.LittleEndian.PutUint32(wav[4:8], providerRIFFSizeMarker)
	binary.LittleEndian.PutUint32(wav[40:44], providerDataSizeMarker)
}
