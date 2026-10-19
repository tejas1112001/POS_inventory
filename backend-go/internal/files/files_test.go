package files

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := range w {
		img.Set(x, 0, color.RGBA{R: 200, A: 255})
	}
	var b bytes.Buffer
	require.NoError(t, png.Encode(&b, img))
	return b.Bytes()
}

func dataURI(mime string, b []byte) string {
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(b)
}

// A 1x1 lossy WebP.
const webp1x1 = "UklGRiQAAABXRUJQVlA4IBgAAAAwAQCdASoBAAEAAwA0JaQAA3AA/vuUAAA="

func TestProcessImageAccepts(t *testing.T) {
	img, err := ProcessImage(pngBytes(t, 32, 16))
	require.NoError(t, err)
	require.Equal(t, "image/png", img.ContentType)
	require.Equal(t, 32, img.Width)
	require.Len(t, img.SHA256, 32)

	var jb bytes.Buffer
	require.NoError(t, jpeg.Encode(&jb, image.NewRGBA(image.Rect(0, 0, 8, 8)), nil))
	img, err = ProcessImage(jb.Bytes())
	require.NoError(t, err)
	require.Equal(t, "image/jpeg", img.ContentType)
	require.Equal(t, "jpg", img.Ext)

	wb, err := base64.StdEncoding.DecodeString(webp1x1)
	require.NoError(t, err)
	img, err = ProcessImage(wb)
	require.NoError(t, err)
	require.Equal(t, "image/png", img.ContentType, "WebP is re-encoded as PNG")
}

func TestProcessImageRejects(t *testing.T) {
	var gb bytes.Buffer
	require.NoError(t, gif.Encode(&gb, image.NewPaletted(image.Rect(0, 0, 2, 2), color.Palette{color.Black, color.White}), nil))
	cases := map[string][]byte{
		"svg":       []byte(`<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"></svg>`),
		"gif":       gb.Bytes(),
		"html":      []byte("<html><script>alert(1)</script></html>"),
		"empty":     nil,
		"too wide":  pngBytes(t, MaxImageSide+1, 1),
		"truncated": pngBytes(t, 10, 10)[:40],
		"too large": bytes.Repeat([]byte{0}, MaxImageBytes+1),
	}
	for name, b := range cases {
		_, err := ProcessImage(b)
		require.ErrorIs(t, err, ErrBadImage, name)
	}
}

func TestReencodeStripsTrailingData(t *testing.T) {
	polyglot := append(pngBytes(t, 4, 4), []byte("<script>alert(1)</script>")...)
	img, err := ProcessImage(polyglot)
	require.NoError(t, err)
	require.NotContains(t, string(img.Data), "<script>")
}

func TestParseDataURI(t *testing.T) {
	p := pngBytes(t, 2, 2)
	b, err := ParseDataURI(dataURI("image/png", p))
	require.NoError(t, err)
	require.Equal(t, p, b)
	for _, bad := range []string{"", "http://x/y.png", "data:image/png,rawnotbase64", "data:image/png;base64,***"} {
		_, err := ParseDataURI(bad)
		require.ErrorIs(t, err, ErrBadImage, bad)
	}
}

func TestFSStorage(t *testing.T) {
	ctx := context.Background()
	s, err := NewFSStorage(t.TempDir())
	require.NoError(t, err)
	key := "tenants/abcdefghij/images/1f2e3d4c-0000-4000-8000-000000000000.png"
	require.NoError(t, s.Put(ctx, key, "image/png", []byte("hello")))
	rc, size, err := s.Get(ctx, key)
	require.NoError(t, err)
	b, _ := io.ReadAll(rc)
	_ = rc.Close()
	require.Equal(t, int64(5), size)
	require.Equal(t, "hello", string(b))
	require.NoError(t, s.Delete(ctx, key))
	_, _, err = s.Get(ctx, key)
	require.ErrorIs(t, err, ErrNotFound)

	for _, bad := range []string{"../etc/passwd", "/abs/key", "tenants/../../x", "tenants/A/x", "a b"} {
		require.Error(t, s.Put(ctx, bad, "image/png", []byte("x")), bad)
	}
}

func TestURLs(t *testing.T) {
	id := [16]byte{1}
	require.Equal(t, "/files/t/01000000-0000-0000-0000-000000000000", URL(id, PurposeStoreLogo))
	require.Equal(t, "/api/files/01000000-0000-0000-0000-000000000000", URL(id, PurposeCustomerImage))
	got, ok := parseFileURL(URL(id, PurposeCategoryImage), PurposeCategoryImage)
	require.True(t, ok)
	require.Equal(t, [16]byte(got), id)
	_, ok = parseFileURL(URL(id, PurposeCategoryImage), PurposeStoreLogo)
	require.False(t, ok, "URL of another purpose family")
}
