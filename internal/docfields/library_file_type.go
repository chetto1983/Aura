package docfields

import (
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/documents/filecard"
)

var fileTypeGroups = []struct {
	kind       string
	document   bool
	extensions []string
}{
	{"pdf", true, strings.Fields("pdf")},
	{"word", true, strings.Fields("doc docx docm dot dotx dotm odt ott rtf")},
	{"spreadsheet", true, strings.Fields("xls xlsx xlsm xlsb xlt xltx xltm ods ots csv tsv")},
	{"presentation", true, strings.Fields("ppt pptx pptm pps ppsx ppsm pot potx potm odp otp")},
	{"text", true, strings.Fields("txt md markdown log rst")},
	{"image", false, strings.Fields("jpg jpeg png gif webp tif tiff bmp svg avif heic heif ico")},
	{"video", false, strings.Fields("mp4 webm mkv mov avi m4v mpeg mpg ogv wmv 3gp")},
	{"audio", false, strings.Fields("mp3 wav flac oga ogg opus m4a aac aiff aif wma amr")},
	{"code", false, codeExtensions()},
}

func codeExtensions() []string {
	// JSON/XML and HTML have dedicated ingest handlers outside the source-language registry.
	extensions := []string{"json", "xml", "html", "htm"}
	for _, extension := range filecard.SourceExtensions() {
		extensions = append(extensions, strings.TrimPrefix(extension, "."))
	}
	return extensions
}

// NormalizeFileType resolves an omitted filter to the Documents category.
func NormalizeFileType(kind string) string {
	if kind == "" {
		return "documents"
	}
	return kind
}

func validFileType(kind string) bool {
	kind = NormalizeFileType(kind)
	if kind == "all" || kind == "documents" {
		return true
	}
	for _, group := range fileTypeGroups {
		if group.kind == kind {
			return true
		}
	}
	return false
}

func fileTypeFromName(name string) string {
	name = strings.ToLower(name)
	for _, group := range fileTypeGroups {
		for _, extension := range group.extensions {
			if strings.HasSuffix(name, "."+extension) {
				return group.kind
			}
		}
	}
	return "other"
}

func fileTypePredicate(kind string, params map[string]any) string {
	kind = NormalizeFileType(kind)
	if kind == "all" {
		return ""
	}
	var alternatives []string
	for _, group := range fileTypeGroups {
		included := kind == group.kind || kind == "documents" && group.document
		if !included {
			continue
		}
		for _, extension := range group.extensions {
			param := fmt.Sprintf("file_extension_%d", len(alternatives))
			params[param] = "%." + extension
			alternatives = append(alternatives, "file_name.toLowerCase() LIKE :"+param)
		}
	}
	return "(" + strings.Join(alternatives, " OR ") + ")"
}
