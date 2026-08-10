package qiniu

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/gorilla/websocket"
)

const (
	asrMessageConfiguration = 0x1
	asrMessageAudio         = 0x2
	asrMessageResponse      = 0x9
	asrMessageError         = 0xf

	asrFlagSequence = 0x1
	asrFlagFinal    = 0x3
	asrJSON         = 0x1
	asrGZIP         = 0x1
)

type asrConfiguration struct {
	User struct {
		UID string `json:"uid"`
	} `json:"user"`
	Audio struct {
		Format     string `json:"format"`
		SampleRate int    `json:"sample_rate"`
		Bits       int    `json:"bits"`
		Channel    int    `json:"channel"`
		Codec      string `json:"codec"`
	} `json:"audio"`
	Request struct {
		ModelName  string `json:"model_name"`
		EnablePunc bool   `json:"enable_punc"`
	} `json:"request"`
}

type asrResponse struct {
	RequestID string `json:"reqid"`
	Result    struct {
		Text string `json:"text"`
	} `json:"result"`
}

func encodeASRConfiguration(requestID string, model string) ([]byte, error) {
	configuration := asrConfiguration{}
	configuration.User.UID = requestID
	configuration.Audio.Format = "pcm"
	configuration.Audio.SampleRate = qiniuASRSampleRate
	configuration.Audio.Bits = 16
	configuration.Audio.Channel = 1
	configuration.Audio.Codec = "raw"
	configuration.Request.ModelName = model
	configuration.Request.EnablePunc = true
	payload, err := json.Marshal(configuration)
	if err != nil {
		return nil, err
	}
	compressed, err := gzipASRPayload(payload)
	if err != nil {
		return nil, err
	}
	return encodeASRFrame(
		asrMessageConfiguration,
		asrFlagSequence,
		asrJSON,
		asrGZIP,
		1,
		compressed,
	), nil
}

func encodeASRAudioFrame(sequence int32, pcm []byte, final bool) ([]byte, error) {
	compressed, err := gzipASRPayload(pcm)
	if err != nil {
		return nil, err
	}
	flags := byte(asrFlagSequence)
	if final {
		flags = asrFlagFinal
	}
	return encodeASRFrame(
		asrMessageAudio,
		flags,
		0,
		asrGZIP,
		sequence,
		compressed,
	), nil
}

func encodeASRFrame(
	messageType byte,
	flags byte,
	serialization byte,
	compression byte,
	sequence int32,
	payload []byte,
) []byte {
	frame := make([]byte, 12+len(payload))
	frame[0] = 0x11
	frame[1] = messageType<<4 | flags
	frame[2] = serialization<<4 | compression
	binary.BigEndian.PutUint32(frame[4:8], uint32(sequence))
	binary.BigEndian.PutUint32(frame[8:12], uint32(len(payload)))
	copy(frame[12:], payload)
	return frame
}

func collectASRResponses(
	ctx context.Context,
	connection *websocket.Conn,
	observer asrObserver,
) (string, string, error) {
	var (
		latestText      string
		latestRequestID string
		lastSequence    int64
		finalEmitted    bool
	)
	for {
		messageType, frame, err := connection.ReadMessage()
		if err != nil {
			return "", "", transportASRError(ctx, err)
		}
		if messageType != websocket.BinaryMessage || len(frame) > qiniuASRMaxFrameSize {
			return "", "", invalidASRResponse("Qiniu ASR response frame is invalid")
		}
		decoded, err := decodeASRResponseFrame(frame)
		if err != nil {
			return "", "", err
		}
		if decoded.requestID != "" {
			latestRequestID = decoded.requestID
		}
		absoluteSequence := int64(decoded.sequence)
		if absoluteSequence < 0 {
			absoluteSequence = -absoluteSequence
		}
		if absoluteSequence <= lastSequence {
			return "", "", invalidASRResponse(
				"Qiniu ASR response sequence is not increasing",
			)
		}
		lastSequence = absoluteSequence
		text := strings.TrimSpace(decoded.text)
		if text != "" && text != latestText {
			latestText = text
			if observer != nil {
				if err := observer(ctx, asrUpdate{
					transcript: latestText,
					final:      decoded.final,
				}); err != nil {
					return "", "", &asrError{
						kind:  asrErrorCancelled,
						cause: errors.New("Qiniu ASR observer rejected an update"),
					}
				}
				finalEmitted = decoded.final
			}
		}
		if decoded.final {
			if latestText == "" {
				return "", "", invalidASRResponse(
					"Qiniu ASR final response has no transcript",
				)
			}
			if observer != nil && !finalEmitted {
				if err := observer(ctx, asrUpdate{
					transcript: latestText,
					final:      true,
				}); err != nil {
					return "", "", &asrError{
						kind:  asrErrorCancelled,
						cause: errors.New("Qiniu ASR observer rejected the final update"),
					}
				}
			}
			return latestRequestID, latestText, nil
		}
	}
}

type decodedASRResponse struct {
	sequence  int32
	requestID string
	text      string
	final     bool
}

func decodeASRResponseFrame(frame []byte) (decodedASRResponse, error) {
	if len(frame) < 4 {
		return decodedASRResponse{}, invalidASRResponse(
			"Qiniu ASR response header is incomplete",
		)
	}
	version := frame[0] >> 4
	headerWords := int(frame[0] & 0x0f)
	headerSize := headerWords * 4
	messageType := frame[1] >> 4
	flags := frame[1] & 0x0f
	serialization := frame[2] >> 4
	compression := frame[2] & 0x0f
	if version != 1 || headerWords < 1 || headerSize > len(frame) ||
		serialization != asrJSON || (compression != 0 && compression != asrGZIP) {
		return decodedASRResponse{}, invalidASRResponse(
			"Qiniu ASR response header is unsupported",
		)
	}
	payload := frame[headerSize:]
	if messageType == asrMessageError {
		return decodedASRResponse{}, &asrError{
			kind:  asrErrorUnavailable,
			cause: errors.New("Qiniu ASR returned a provider error"),
		}
	}
	if messageType != asrMessageResponse ||
		(flags != asrFlagSequence && flags != asrFlagFinal) ||
		len(payload) < 8 {
		return decodedASRResponse{}, invalidASRResponse(
			"Qiniu ASR response message is invalid",
		)
	}
	sequence := int32(binary.BigEndian.Uint32(payload[:4]))
	payloadSize := int(binary.BigEndian.Uint32(payload[4:8]))
	payload = payload[8:]
	if payloadSize < 0 || payloadSize != len(payload) {
		return decodedASRResponse{}, invalidASRResponse(
			"Qiniu ASR response payload size is invalid",
		)
	}
	if compression == asrGZIP {
		var err error
		payload, err = gunzipASRPayload(payload)
		if err != nil {
			return decodedASRResponse{}, invalidASRResponse(
				"Qiniu ASR response compression is invalid",
			)
		}
	}
	var response asrResponse
	if err := json.Unmarshal(payload, &response); err != nil {
		return decodedASRResponse{}, invalidASRResponse(
			"Qiniu ASR response JSON is invalid",
		)
	}
	requestID := sanitizeASRIdentifier(response.RequestID)
	if response.RequestID != "" && requestID == "" {
		return decodedASRResponse{}, invalidASRResponse(
			"Qiniu ASR response request identifier is invalid",
		)
	}
	final := flags == asrFlagFinal || sequence < 0
	if final && sequence >= 0 {
		return decodedASRResponse{}, invalidASRResponse(
			"Qiniu ASR final response sequence is invalid",
		)
	}
	if !final && sequence <= 0 {
		return decodedASRResponse{}, invalidASRResponse(
			"Qiniu ASR response sequence is invalid",
		)
	}
	return decodedASRResponse{
		sequence:  sequence,
		requestID: requestID,
		text:      response.Result.Text,
		final:     final,
	}, nil
}

func gzipASRPayload(payload []byte) ([]byte, error) {
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(payload); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return compressed.Bytes(), nil
}

func gunzipASRPayload(payload []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	decoded, err := io.ReadAll(io.LimitReader(reader, qiniuASRMaxFrameSize+1))
	if err != nil || len(decoded) > qiniuASRMaxFrameSize {
		return nil, errors.New("Qiniu ASR response exceeds the accepted limit")
	}
	return decoded, nil
}

func invalidASRResponse(message string) *asrError {
	return &asrError{
		kind:  asrErrorInvalidResponse,
		cause: errors.New(message),
	}
}
