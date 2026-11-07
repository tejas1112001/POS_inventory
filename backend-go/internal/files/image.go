package files

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"strings"

	"github.com/gabriel-vasile/mimetype"
	_ "golang.org/x/image/webp" // registers the WebP decoder
)

// Limits for uploaded images (C-17).
const (
	MaxImageBytes  = 2 << 20 // 2 MiB decoded
	MaxImageSide   = 4096
	MaxImagePixels = 16 << 20
	jpegQuality    = 85
)

// ErrBadImage is returned for anything that is not a valid PNG, JPEG or WebP
// within the limits. The message is safe to show.
var ErrBadImage = errors.New("must be a PNG, JPEG or WebP image up to 2 MiB and 4096x4096")

// Image is a validated, re-encoded image ready to store.
type Image struct {
	Data        []byte
	ContentType string // image/png or image/jpeg
	Ext         string // png or jpg
	SHA256      []byte
	Width       int
	Height      int
}

// ParseDataURI decodes "data:<mime>;base64,<data>". The declared MIME type
// is ignored: the bytes are sniffed.
func ParseDataURI(s string) ([]byte, error) {
	rest, ok := strings.CutPrefix(s, "data:")
	if !ok {
		return nil, ErrBadImage
	}
	meta, payload, ok := strings.Cut(rest, ",")
	if !ok || !strings.HasSuffix(meta, ";base64") {
		return nil, ErrBadImage
	}
	if base64.StdEncoding.DecodedLen(len(payload)) > MaxImageBytes+3 {
		return nil, ErrBadImage
	}
	b, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return nil, ErrBadImage
	}
	return b, nil
}

// ProcessImage validates raw bytes and re-encodes them. Re-encoding drops
// metadata (EXIF location, comments) and anything appended to the file.
// WebP is re-encoded as PNG (there is no pure-Go WebP encoder). SVG, GIF
// and everything else are rejected.
func ProcessImage(raw []byte) (Image, error) {
	if len(raw) == 0 || len(raw) > MaxImageBytes {
		return Image{}, ErrBadImage
	}
	mt := mimetype.Detect(raw).String()
	switch mt {
	case "image/png", "image/jpeg", "image/webp":
	default:
		return Image{}, ErrBadImage
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 ||
		cfg.Width > MaxImageSide || cfg.Height > MaxImageSide || cfg.Width*cfg.Height > MaxImagePixels {
		return Image{}, ErrBadImage
	}
	if (mt == "image/png" && format != "png") || (mt == "image/jpeg" && format != "jpeg") || (mt == "image/webp" && format != "webp") {
		return Image{}, ErrBadImage
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return Image{}, ErrBadImage
	}
	var out bytes.Buffer
	res := Image{Width: cfg.Width, Height: cfg.Height}
	if format == "jpeg" {
		if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: jpegQuality}); err != nil {
			return Image{}, ErrBadImage
		}
		res.ContentType, res.Ext = "image/jpeg", "jpg"
	} else {
		enc := png.Encoder{CompressionLevel: png.BestCompression}
		if err := enc.Encode(&out, img); err != nil {
			return Image{}, ErrBadImage
		}
		res.ContentType, res.Ext = "image/png", "png"
	}
	res.Data = out.Bytes()
	sum := sha256.Sum256(res.Data)
	res.SHA256 = sum[:]
	return res, nil
}
