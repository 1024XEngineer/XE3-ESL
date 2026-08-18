package image

import (
	"bytes"
	"encoding/binary"
	stdimage "image"
	"image/jpeg"
	"image/png"
	"strings"

	_ "golang.org/x/image/webp"
)

const maxImageDimension = 16_384

type normalizedImage struct {
	payload     []byte
	contentType string
	extension   string
	width       int
	height      int
}

// NormalizePayload validates an untrusted image, strips metadata, applies JPEG
// orientation, and returns a canonical payload shared by image-owning domains.
func NormalizePayload(
	declaredContentType string,
	payload []byte,
) ([]byte, string, int, int, error) {
	normalized, err := normalizeImage(declaredContentType, payload)
	if err != nil {
		return nil, "", 0, 0, err
	}
	return normalized.payload, normalized.contentType,
		normalized.width, normalized.height, nil
}

func normalizeImage(
	declaredContentType string,
	payload []byte,
) (normalizedImage, error) {
	configuration, format, err := stdimage.DecodeConfig(bytes.NewReader(payload))
	if err != nil || configuration.Width < 1 || configuration.Height < 1 {
		return normalizedImage{}, ErrInvalid
	}
	if configuration.Width > maxImageDimension ||
		configuration.Height > maxImageDimension ||
		int64(configuration.Width)*int64(configuration.Height) >
			MaxPixels {
		return normalizedImage{}, ErrTooLarge
	}
	var declaredExpected string
	switch format {
	case "jpeg":
		declaredExpected = "image/jpeg"
	case "png":
		declaredExpected = "image/png"
	case "webp":
		declaredExpected = "image/webp"
	default:
		return normalizedImage{}, ErrUnsupported
	}
	if strings.TrimSpace(declaredContentType) != declaredExpected {
		return normalizedImage{}, ErrInvalid
	}
	if animatedImage(format, payload) {
		return normalizedImage{}, ErrUnsupported
	}

	decoded, decodedFormat, err := stdimage.Decode(bytes.NewReader(payload))
	if err != nil || decodedFormat != format {
		return normalizedImage{}, ErrInvalid
	}
	if format == "jpeg" {
		decoded = applyEXIFOrientation(decoded, jpegEXIFOrientation(payload))
	}

	bounds := decoded.Bounds()
	result := normalizedImage{
		width:  bounds.Dx(),
		height: bounds.Dy(),
	}
	var output bytes.Buffer
	switch format {
	case "jpeg":
		result.contentType = "image/jpeg"
		result.extension = ".jpg"
		err = jpeg.Encode(&output, decoded, &jpeg.Options{Quality: 90})
	case "png", "webp":
		// Re-encoding removes metadata. WebP is normalized to PNG because the
		// standard image stack provides a decoder but no production encoder.
		result.contentType = "image/png"
		result.extension = ".png"
		err = png.Encode(&output, decoded)
	}
	if err != nil || output.Len() == 0 {
		return normalizedImage{}, ErrInvalid
	}
	if output.Len() > MaxBytes {
		return normalizedImage{}, ErrTooLarge
	}
	result.payload = output.Bytes()
	return result, nil
}

func animatedImage(format string, payload []byte) bool {
	switch format {
	case "png":
		// APNG uses an animation-control chunk before its frame data.
		for offset := 8; offset+12 <= len(payload); {
			chunkLength := uint64(binary.BigEndian.Uint32(payload[offset : offset+4]))
			chunkEnd := uint64(offset) + 12 + chunkLength
			if chunkEnd > uint64(len(payload)) {
				return false
			}
			chunkType := string(payload[offset+4 : offset+8])
			if chunkType == "acTL" {
				return true
			}
			if chunkType == "IEND" {
				return false
			}
			offset = int(chunkEnd)
		}
	case "webp":
		// Animated WebP sets the animation bit in VP8X and carries ANIM/ANMF
		// chunks. Check both so malformed combinations cannot bypass the rule.
		for offset := 12; offset+8 <= len(payload); {
			chunkLength := uint64(binary.LittleEndian.Uint32(payload[offset+4 : offset+8]))
			chunkEnd := uint64(offset) + 8 + chunkLength
			if chunkEnd > uint64(len(payload)) {
				return false
			}
			chunkType := string(payload[offset : offset+4])
			if chunkType == "ANIM" || chunkType == "ANMF" ||
				chunkType == "VP8X" && chunkLength > 0 &&
					payload[offset+8]&0x02 != 0 {
				return true
			}
			if chunkLength%2 == 1 {
				chunkEnd++
			}
			if chunkEnd > uint64(len(payload)) {
				return false
			}
			offset = int(chunkEnd)
		}
	}
	return false
}

func jpegEXIFOrientation(payload []byte) int {
	if len(payload) < 4 || payload[0] != 0xff || payload[1] != 0xd8 {
		return 1
	}
	for offset := 2; offset+4 <= len(payload); {
		if payload[offset] != 0xff {
			return 1
		}
		for offset < len(payload) && payload[offset] == 0xff {
			offset++
		}
		if offset >= len(payload) {
			return 1
		}
		marker := payload[offset]
		offset++
		if marker == 0xd9 || marker == 0xda {
			return 1
		}
		if marker == 0x01 || marker >= 0xd0 && marker <= 0xd7 {
			continue
		}
		if offset+2 > len(payload) {
			return 1
		}
		segmentLength := int(binary.BigEndian.Uint16(payload[offset : offset+2]))
		if segmentLength < 2 || offset+segmentLength > len(payload) {
			return 1
		}
		segment := payload[offset+2 : offset+segmentLength]
		if marker == 0xe1 && bytes.HasPrefix(segment, []byte("Exif\x00\x00")) {
			return tiffOrientation(segment[6:])
		}
		offset += segmentLength
	}
	return 1
}

func tiffOrientation(tiff []byte) int {
	if len(tiff) < 8 {
		return 1
	}
	var order binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 1
	}
	if order.Uint16(tiff[2:4]) != 42 {
		return 1
	}
	ifdOffset := uint64(order.Uint32(tiff[4:8]))
	if ifdOffset+2 > uint64(len(tiff)) {
		return 1
	}
	entryCount := uint64(order.Uint16(tiff[ifdOffset : ifdOffset+2]))
	entriesStart := ifdOffset + 2
	if entryCount > (uint64(len(tiff))-entriesStart)/12 {
		return 1
	}
	for index := uint64(0); index < entryCount; index++ {
		entryOffset := entriesStart + index*12
		entry := tiff[entryOffset : entryOffset+12]
		if order.Uint16(entry[0:2]) != 0x0112 ||
			order.Uint16(entry[2:4]) != 3 ||
			order.Uint32(entry[4:8]) != 1 {
			continue
		}
		orientation := int(order.Uint16(entry[8:10]))
		if orientation >= 1 && orientation <= 8 {
			return orientation
		}
		return 1
	}
	return 1
}

func applyEXIFOrientation(source stdimage.Image, orientation int) stdimage.Image {
	if orientation <= 1 || orientation > 8 {
		return source
	}
	sourceBounds := source.Bounds()
	width, height := sourceBounds.Dx(), sourceBounds.Dy()
	outputWidth, outputHeight := width, height
	if orientation >= 5 {
		outputWidth, outputHeight = height, width
	}
	output := stdimage.NewNRGBA(stdimage.Rect(0, 0, outputWidth, outputHeight))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			var outputX, outputY int
			switch orientation {
			case 2:
				outputX, outputY = width-1-x, y
			case 3:
				outputX, outputY = width-1-x, height-1-y
			case 4:
				outputX, outputY = x, height-1-y
			case 5:
				outputX, outputY = y, x
			case 6:
				outputX, outputY = height-1-y, x
			case 7:
				outputX, outputY = height-1-y, width-1-x
			case 8:
				outputX, outputY = y, width-1-x
			}
			output.Set(
				outputX,
				outputY,
				source.At(sourceBounds.Min.X+x, sourceBounds.Min.Y+y),
			)
		}
	}
	return output
}
