package documents

import (
	"bytes"
	"image"
	_ "image/gif" // Register only the supported raster header formats; Craft renders pixels.
	_ "image/jpeg"
	"io"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

func screenPreviewRaster(source io.Reader) (io.Reader, error) {
	var header bytes.Buffer
	cfg, _, err := image.DecodeConfig(io.TeeReader(io.LimitReader(source, 1<<20), &header))
	if err != nil {
		return nil, ErrPreviewInvalid
	}
	// PhotoCraft's native source-preview ceiling is 64MP. Check it before doc.open
	// can allocate pixels, retaining at most 1MiB of header data to replay into copy-in.
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 32768 || cfg.Height > 32768 || cfg.Width > 67108864/cfg.Height {
		return nil, ErrPreviewTooLarge
	}
	return io.MultiReader(bytes.NewReader(header.Bytes()), source), nil
}
