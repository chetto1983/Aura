package chatgpt

import (
	"errors"
	"io"
	"net/http"

	"github.com/openai/openai-go/v3/option"
)

const maxCatalogBytes = 2 << 20

func boundedModelsMiddleware(req *http.Request, next option.MiddlewareNext) (*http.Response, error) {
	response, err := next(req)
	if err != nil || response == nil || response.Body == nil {
		return response, err
	}
	response.Body = &boundedCatalogBody{ReadCloser: response.Body, remaining: maxCatalogBytes}
	return response, nil
}

type boundedCatalogBody struct {
	io.ReadCloser
	remaining int
}

func (b *boundedCatalogBody) Read(p []byte) (int, error) {
	if len(p) > b.remaining+1 {
		p = p[:b.remaining+1]
	}
	n, err := b.ReadCloser.Read(p)
	b.remaining -= n
	if b.remaining < 0 {
		return 0, errors.New("chatgpt: model catalog exceeds size limit")
	}
	return n, err
}
