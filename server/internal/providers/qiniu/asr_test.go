package qiniu

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	agentvoice "github.com/1024XEngineer/XE3-ESL/server/internal/agent/input/voice"
	platformmedia "github.com/1024XEngineer/XE3-ESL/server/internal/platform/media"
	"github.com/gorilla/websocket"
)

func TestQiniuASRUsesBinaryProtocolAndStreamsSnapshots(t *testing.T) {
	t.Parallel()
	const apiKey = "qiniu-asr-test-key"
	pcm := bytes.Repeat([]byte{1, 2, 3, 4}, qiniuASRChunkBytes/4+25)
	serverErrors := make(chan error, 1)
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		connection, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			serverErrors <- err
			return
		}
		defer connection.Close()
		if request.Header.Get("Authorization") != "Bearer "+apiKey {
			serverErrors <- errors.New("authorization header is invalid")
			return
		}
		configuration, err := readTestASRFrame(connection)
		if err != nil {
			serverErrors <- err
			return
		}
		if configuration.messageType != asrMessageConfiguration ||
			configuration.flags != asrFlagSequence || configuration.sequence != 1 ||
			configuration.serialization != asrJSON ||
			configuration.compression != asrGZIP {
			serverErrors <- fmt.Errorf("unexpected configuration frame: %#v", configuration)
			return
		}
		var config asrConfiguration
		if err := json.Unmarshal(configuration.payload, &config); err != nil {
			serverErrors <- err
			return
		}
		if config.User.UID == "" || config.Audio.SampleRate != qiniuASRSampleRate ||
			config.Audio.Bits != 16 || config.Audio.Channel != 1 ||
			config.Audio.Format != "pcm" || config.Audio.Codec != "raw" ||
			config.Request.ModelName != qiniuASRModel || !config.Request.EnablePunc {
			serverErrors <- fmt.Errorf("unexpected ASR configuration: %#v", config)
			return
		}

		var received bytes.Buffer
		for expectedSequence := int32(2); ; expectedSequence++ {
			frame, readErr := readTestASRFrame(connection)
			if readErr != nil {
				serverErrors <- readErr
				return
			}
			if frame.messageType != asrMessageAudio || frame.compression != asrGZIP ||
				frame.serialization != 0 {
				serverErrors <- fmt.Errorf("unexpected audio frame: %#v", frame)
				return
			}
			received.Write(frame.payload)
			if frame.flags == asrFlagFinal {
				if frame.sequence != -expectedSequence {
					serverErrors <- fmt.Errorf("final sequence = %d", frame.sequence)
					return
				}
				break
			}
			if frame.flags != asrFlagSequence || frame.sequence != expectedSequence {
				serverErrors <- fmt.Errorf("audio sequence = %d", frame.sequence)
				return
			}
		}
		if !bytes.Equal(received.Bytes(), pcm) {
			serverErrors <- errors.New("received PCM differs from source")
			return
		}
		if err := writeTestASRResponse(
			connection,
			2,
			asrFlagSequence,
			"qiniu-request-1",
			"I practice",
		); err != nil {
			serverErrors <- err
			return
		}
		if err := writeTestASRResponse(
			connection,
			-3,
			asrFlagFinal,
			"qiniu-request-1",
			"I practice English.",
		); err != nil {
			serverErrors <- err
			return
		}
		serverErrors <- nil
	}))
	defer server.Close()

	client := mustTestASRClient(t, apiKey)
	client.endpoint = "ws" + strings.TrimPrefix(server.URL, "http")
	var updates []asrUpdate
	result, err := client.transcribePCM(
		context.Background(),
		bytes.NewReader(pcm),
		qiniuASRSampleRate,
		func(_ context.Context, update asrUpdate) error {
			updates = append(updates, update)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("transcribe PCM: %v", err)
	}
	if serverErr := <-serverErrors; serverErr != nil {
		t.Fatalf("test server: %v", serverErr)
	}
	if result.id != "qiniu-request-1" ||
		result.transcript != "I practice English." ||
		result.audioSeconds != 1 {
		t.Fatalf("result = %#v", result)
	}
	if len(updates) != 2 || updates[0].transcript != "I practice" ||
		updates[0].final || !updates[1].final ||
		updates[1].transcript != result.transcript {
		t.Fatalf("updates = %#v", updates)
	}
}

func TestQiniuAgentAdapterAcceptsValidatedWAV(t *testing.T) {
	t.Parallel()
	pcm := bytes.Repeat([]byte{1, 2}, qiniuASRSampleRate)
	server := newFinalASRTestServer(t, pcm, "你好，English。")
	defer server.Close()
	client := mustTestASRClient(t, "adapter-key")
	client.endpoint = "ws" + strings.TrimPrefix(server.URL, "http")
	recognizer := &AgentVoiceRecognizer{client: client}
	result, err := recognizer.Transcribe(
		context.Background(),
		agentvoice.TranscriptionRequest{Audio: newTestAudioSource(pcm)},
	)
	if err != nil {
		t.Fatalf("transcribe WAV: %v", err)
	}
	if result.Provider != qiniuProviderName || result.Model != qiniuASRModel ||
		result.Transcript != "你好，English。" || result.Usage.AudioSeconds != 1 {
		t.Fatalf("result = %#v", result)
	}
}

func TestQiniuASRRejectsUnsafeConfigurationAndRedactsSecret(t *testing.T) {
	t.Parallel()
	tests := []ASRConfig{
		{BaseURL: "ws://api.qnaigc.com/v1/voice/asr", Model: qiniuASRModel, Timeout: time.Second},
		{BaseURL: qiniuASREndpoint + "?key=secret", Model: qiniuASRModel, Timeout: time.Second},
		{BaseURL: qiniuASREndpoint, Model: "other", Timeout: time.Second},
		{BaseURL: qiniuASREndpoint, Model: qiniuASRModel, Timeout: 0},
	}
	for _, config := range tests {
		if client, err := newASR(config, "safe-key"); err == nil || client != nil {
			t.Fatalf("unsafe config accepted: %#v", config)
		}
	}
	if client, err := newASR(testASRConfig(), "unsafe key"); err == nil || client != nil {
		t.Fatal("unsafe API key was accepted")
	}
	client := mustTestASRClient(t, "do-not-print-this-key")
	for _, output := range []string{client.String(), fmt.Sprintf("%#v", client)} {
		if strings.Contains(output, "do-not-print-this-key") ||
			!strings.Contains(output, "[REDACTED]") {
			t.Fatalf("unsafe client rendering: %q", output)
		}
	}
}

func TestDecodeQiniuASRResponseRejectsMalformedFrames(t *testing.T) {
	t.Parallel()
	validPayload, err := json.Marshal(map[string]any{
		"reqid":  "request-1",
		"result": map[string]any{"text": "hello"},
	})
	if err != nil {
		t.Fatal(err)
	}
	compressed, err := gzipASRPayload(validPayload)
	if err != nil {
		t.Fatal(err)
	}
	valid := encodeASRFrame(
		asrMessageResponse,
		asrFlagFinal,
		asrJSON,
		asrGZIP,
		-2,
		compressed,
	)
	malformed := [][]byte{
		{},
		{0x11, 0x91, 0x11, 0},
		append([]byte(nil), valid[:len(valid)-1]...),
		encodeASRFrame(asrMessageResponse, asrFlagFinal, asrJSON, 0, 2, validPayload),
	}
	for _, frame := range malformed {
		if _, err := decodeASRResponseFrame(frame); err == nil {
			t.Fatalf("malformed response accepted: %x", frame)
		}
	}
	decoded, err := decodeASRResponseFrame(valid)
	if err != nil || !decoded.final || decoded.text != "hello" {
		t.Fatalf("decoded response = %#v, %v", decoded, err)
	}
}

func TestQiniuASRErrorMappingDoesNotExposeProviderPayload(t *testing.T) {
	t.Parallel()
	secretPayload := "provider-secret-audio-transcript"
	failure := invalidASRResponse(secretPayload)
	mapped := mapAgentASRError(failure)
	if strings.Contains(mapped.Error(), secretPayload) ||
		strings.Contains(fmt.Sprintf("%v", mapped), secretPayload) {
		t.Fatalf("mapped error exposed provider payload: %v", mapped)
	}
	var speechError *agentvoice.SpeechError
	if !errors.As(mapped, &speechError) || speechError.Kind != agentvoice.ErrorInvalidResponse {
		t.Fatalf("mapped error = %#v", mapped)
	}
}

func TestQiniuASRMapsCancellationAndTimeout(t *testing.T) {
	t.Parallel()
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		connection, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		defer connection.Close()
		for {
			if _, _, err := connection.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer server.Close()

	client := mustTestASRClient(t, "timeout-key")
	client.endpoint = "ws" + strings.TrimPrefix(server.URL, "http")
	client.timeout = 50 * time.Millisecond
	_, err := client.transcribePCM(
		context.Background(),
		bytes.NewReader([]byte{1, 2}),
		qiniuASRSampleRate,
		nil,
	)
	var timeoutFailure *asrError
	if !errors.As(err, &timeoutFailure) || timeoutFailure.kind != asrErrorTimeout {
		t.Fatalf("timeout error = %#v", err)
	}

	cancelledContext, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.transcribePCM(
		cancelledContext,
		bytes.NewReader([]byte{1, 2}),
		qiniuASRSampleRate,
		nil,
	)
	var cancelledFailure *asrError
	if !errors.As(err, &cancelledFailure) ||
		cancelledFailure.kind != asrErrorCancelled {
		t.Fatalf("cancellation error = %#v", err)
	}
}

type testASRFrame struct {
	messageType   byte
	flags         byte
	serialization byte
	compression   byte
	sequence      int32
	payload       []byte
}

func readTestASRFrame(connection *websocket.Conn) (testASRFrame, error) {
	messageType, raw, err := connection.ReadMessage()
	if err != nil {
		return testASRFrame{}, err
	}
	if messageType != websocket.BinaryMessage || len(raw) < 12 || raw[0] != 0x11 {
		return testASRFrame{}, errors.New("invalid client ASR frame")
	}
	payloadSize := int(binary.BigEndian.Uint32(raw[8:12]))
	if payloadSize != len(raw)-12 {
		return testASRFrame{}, errors.New("invalid client ASR payload size")
	}
	payload := raw[12:]
	compression := raw[2] & 0x0f
	if compression == asrGZIP {
		payload, err = gunzipASRPayload(payload)
		if err != nil {
			return testASRFrame{}, err
		}
	}
	return testASRFrame{
		messageType:   raw[1] >> 4,
		flags:         raw[1] & 0x0f,
		serialization: raw[2] >> 4,
		compression:   compression,
		sequence:      int32(binary.BigEndian.Uint32(raw[4:8])),
		payload:       payload,
	}, nil
}

func writeTestASRResponse(
	connection *websocket.Conn,
	sequence int32,
	flags byte,
	requestID string,
	text string,
) error {
	payload, err := json.Marshal(map[string]any{
		"reqid":  requestID,
		"result": map[string]any{"text": text},
	})
	if err != nil {
		return err
	}
	compressed, err := gzipASRPayload(payload)
	if err != nil {
		return err
	}
	return connection.WriteMessage(
		websocket.BinaryMessage,
		encodeASRFrame(
			asrMessageResponse,
			flags,
			asrJSON,
			asrGZIP,
			sequence,
			compressed,
		),
	)
}

func newFinalASRTestServer(
	t *testing.T,
	expectedPCM []byte,
	transcript string,
) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{}
	return httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		connection, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer connection.Close()
		if _, err := readTestASRFrame(connection); err != nil {
			t.Errorf("read config: %v", err)
			return
		}
		var received bytes.Buffer
		for {
			frame, err := readTestASRFrame(connection)
			if err != nil {
				t.Errorf("read audio: %v", err)
				return
			}
			received.Write(frame.payload)
			if frame.flags == asrFlagFinal {
				break
			}
		}
		if !bytes.Equal(received.Bytes(), expectedPCM) {
			t.Error("WAV PCM extraction differs from expected bytes")
			return
		}
		if err := writeTestASRResponse(
			connection,
			-2,
			asrFlagFinal,
			"adapter-request",
			transcript,
		); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
}

type testAudioSource struct {
	data []byte
}

func newTestAudioSource(pcm []byte) *testAudioSource {
	dataSize := len(pcm)
	wav := make([]byte, 44+dataSize)
	copy(wav[:4], "RIFF")
	binary.LittleEndian.PutUint32(wav[4:8], uint32(len(wav)-8))
	copy(wav[8:12], "WAVE")
	copy(wav[12:16], "fmt ")
	binary.LittleEndian.PutUint32(wav[16:20], 16)
	binary.LittleEndian.PutUint16(wav[20:22], 1)
	binary.LittleEndian.PutUint16(wav[22:24], 1)
	binary.LittleEndian.PutUint32(wav[24:28], qiniuASRSampleRate)
	binary.LittleEndian.PutUint32(wav[28:32], qiniuASRSampleRate*2)
	binary.LittleEndian.PutUint16(wav[32:34], 2)
	binary.LittleEndian.PutUint16(wav[34:36], 16)
	copy(wav[36:40], "data")
	binary.LittleEndian.PutUint32(wav[40:44], uint32(dataSize))
	copy(wav[44:], pcm)
	return &testAudioSource{data: wav}
}

func (source *testAudioSource) Open() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(source.data)), nil
}

func (source *testAudioSource) MediaType() string { return platformmedia.ContentTypeWAV }
func (source *testAudioSource) Size() int64       { return int64(len(source.data)) }
func (source *testAudioSource) Duration() time.Duration {
	return time.Duration(len(source.data)-44) * time.Second /
		time.Duration(qiniuASRSampleRate*2)
}
func (source *testAudioSource) SampleRate() int { return qiniuASRSampleRate }

func testASRConfig() ASRConfig {
	return ASRConfig{
		BaseURL: qiniuASREndpoint,
		Model:   qiniuASRModel,
		Timeout: 5 * time.Second,
	}
}

func mustTestASRClient(t *testing.T, apiKey string) *asrClient {
	t.Helper()
	client, err := newASR(testASRConfig(), apiKey)
	if err != nil {
		t.Fatalf("new ASR client: %v", err)
	}
	return client
}
