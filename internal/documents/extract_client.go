package documents

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type ExtractClient struct {
	BaseURL string
	Client  *http.Client
}

func (c *ExtractClient) ExtractFile(ctx context.Context, path string, req IngestRequest) (*ExtractorResponse, error) {
	if strings.TrimSpace(c.BaseURL) == "" {
		return nil, fmt.Errorf("document extractor base URL is empty")
	}
	fileName := req.FileName
	if fileName == "" {
		fileName = filepath.Base(path)
	}

	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	errCh := make(chan error, 1)
	go func() {
		defer close(errCh)
		f, err := os.Open(path)
		if err != nil {
			_ = pw.CloseWithError(err)
			errCh <- err
			return
		}
		defer func() { _ = f.Close() }()

		if err = mw.WriteField("source_id", req.SourceID); err != nil {
			_ = pw.CloseWithError(err)
			errCh <- err
			return
		}
		if err = mw.WriteField("source_kind", req.SourceKind); err != nil {
			_ = pw.CloseWithError(err)
			errCh <- err
			return
		}
		if err = mw.WriteField("mime_type", req.MIMEType); err != nil {
			_ = pw.CloseWithError(err)
			errCh <- err
			return
		}
		fw, err := mw.CreateFormFile("file", fileName)
		if err != nil {
			_ = pw.CloseWithError(err)
			errCh <- err
			return
		}
		if _, err = io.Copy(fw, f); err != nil {
			_ = pw.CloseWithError(err)
			errCh <- err
			return
		}
		if err = mw.Close(); err != nil {
			_ = pw.CloseWithError(err)
			errCh <- err
			return
		}
		errCh <- pw.Close()
	}()

	endpoint := strings.TrimRight(c.BaseURL, "/") + "/extract"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, pr)
	if err != nil {
		_ = pr.Close()
		return nil, err
	}
	httpReq.Header.Set("Content-Type", mw.FormDataContentType())

	client := c.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		_ = pr.Close()
		<-errCh
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		_ = pr.Close()
		<-errCh
		return nil, fmt.Errorf("document extractor: HTTP %d", resp.StatusCode)
	}

	var decoded ExtractorResponse
	if err = json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		_ = pr.Close()
		<-errCh
		return nil, fmt.Errorf("document extractor: decode: %w", err)
	}
	if err = <-errCh; err != nil {
		return nil, err
	}
	if err = validateExtractorResponse(&decoded); err != nil {
		return nil, err
	}
	return &decoded, nil
}

func validateExtractorResponse(resp *ExtractorResponse) error {
	if resp == nil {
		return fmt.Errorf("document extractor response is nil")
	}
	if len(resp.Chunks) == 0 {
		return fmt.Errorf("document extractor returned no chunks")
	}
	for i, chunk := range resp.Chunks {
		if NormalizeText(chunk.Text) == "" {
			return fmt.Errorf("document extractor returned empty chunk %d", i)
		}
	}
	return nil
}
