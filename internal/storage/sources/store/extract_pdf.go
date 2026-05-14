package source

import "strings"

func ExtractFromOCRMarkdown(src *Source, md string) ExtractResult {
	pageCount := strings.Count(md, "\n## Page ")
	if strings.HasPrefix(md, "## Page ") {
		pageCount++
	}
	name := "mistral_ocr_adapter"
	if src != nil && src.OCRModel != "" {
		name = "mistral_ocr_adapter:" + src.OCRModel
	}
	body := strings.TrimSpace(md) + "\n"
	return ExtractResult{
		Markdown: body,
		Metadata: ExtractionMeta{
			ExtractorName:    name,
			ExtractorVersion: "pdf_ocr_adapter_v1",
			TextBytes:        len([]byte(body)),
			PageCount:        pageCount,
		},
	}
}
