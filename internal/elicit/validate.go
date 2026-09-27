package elicit

import (
	"maps"
	"math"
	"net/mail"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Closed problem codes keep validator text and submitted values out of the
// route's 422 response, which is retained by the idempotency layer.
const (
	ProblemRequired    = "required"
	ProblemNotAsked    = "not_asked"
	ProblemInvalid     = "invalid"
	ProblemTooShort    = "too_short"
	ProblemTooLong     = "too_long"
	ProblemOutOfRange  = "out_of_range"
	ProblemNotAnOption = "not_an_option"
	ProblemTooFew      = "too_few"
	ProblemTooMany     = "too_many"
	ProblemFormat      = "format"
	ProblemPattern     = "pattern"
)

// FieldErrors maps field names to codes; the empty key means a whole-answer problem.
type FieldErrors map[string]string

func (e FieldErrors) Error() string {
	parts := make([]string, 0, len(e))
	for _, name := range slices.Sorted(maps.Keys(e)) {
		parts = append(parts, name+": "+e[name])
	}
	return strings.Join(parts, "; ")
}

// Validate returns only problem codes. jsonschema-go's own errors quote the
// supplied value, so the projection checks first and never exposes its text.
func Validate(q Question, content map[string]any) FieldErrors {
	errs := FieldErrors{}
	for _, f := range q.Fields {
		value, present := content[f.Name]
		switch {
		case !present && f.Required:
			errs[f.Name] = ProblemRequired
		case present:
			if problem := checkValue(f, value); problem != "" {
				errs[f.Name] = problem
			}
		}
	}
	for name := range content {
		if !slices.ContainsFunc(q.Fields, func(f Field) bool { return f.Name == name }) {
			errs[""] = ProblemNotAsked
		}
	}
	if len(errs) > 0 {
		return errs
	}
	if q.Schema != nil && q.Schema.Validate(content) != nil {
		return FieldErrors{"": ProblemInvalid}
	}
	return nil
}

func checkValue(f Field, value any) string {
	switch f.Kind {
	case KindBoolean:
		if _, ok := value.(bool); !ok {
			return ProblemInvalid
		}
	case KindNumber, KindInteger:
		return checkNumber(f, value)
	case KindString:
		s, ok := value.(string)
		if !ok {
			return ProblemInvalid
		}
		return checkString(f, s)
	case KindEnum:
		return checkChoice(f, value)
	}
	return ""
}

// encoding/json decodes answer numbers as float64.
func checkNumber(f Field, value any) string {
	n, ok := value.(float64)
	if !ok || (f.Kind == KindInteger && n != math.Trunc(n)) {
		return ProblemInvalid
	}
	if (f.Min != nil && n < *f.Min) || (f.Max != nil && n > *f.Max) {
		return ProblemOutOfRange
	}
	return ""
}

func checkString(f Field, s string) string {
	chars := utf8.RuneCountInString(s)
	switch {
	case len(s) > MaxAnswerBytes || (f.MaxLength != nil && chars > *f.MaxLength):
		return ProblemTooLong
	case f.MinLength != nil && chars < *f.MinLength:
		return ProblemTooShort
	case !formatHolds(f.Format, s):
		return ProblemFormat
	case f.Pattern != "" && !patternHolds(f.Pattern, s):
		return ProblemPattern
	}
	return ""
}

// jsonschema-go uses Go regexp matching without implicit anchors.
func patternHolds(pattern, s string) bool {
	ok, err := regexp.MatchString(pattern, s)
	return err == nil && ok
}

func checkChoice(f Field, value any) string {
	if !f.Multi {
		if s, ok := value.(string); !ok || !slices.Contains(f.Enum, s) {
			return ProblemNotAnOption
		}
		return ""
	}
	items, ok := value.([]any)
	if !ok {
		return ProblemInvalid
	}
	for _, item := range items {
		if s, ok := item.(string); !ok || !slices.Contains(f.Enum, s) {
			return ProblemNotAnOption
		}
	}
	switch {
	case f.MinItems != nil && len(items) < *f.MinItems:
		return ProblemTooFew
	case f.MaxItems != nil && len(items) > *f.MaxItems:
		return ProblemTooMany
	}
	return ""
}

// RFC 3986 allows hostless URIs such as file:///tmp and mailto:a@b.
func formatHolds(format, s string) bool {
	switch format {
	case "email":
		addr, err := mail.ParseAddress(s)
		return err == nil && addr.Address == s
	case "uri":
		u, err := url.Parse(s)
		return err == nil && u.Scheme != ""
	case "date":
		_, err := time.Parse(time.DateOnly, s)
		return err == nil
	case "date-time":
		_, err := time.Parse(time.RFC3339, s)
		return err == nil
	default:
		return true
	}
}
