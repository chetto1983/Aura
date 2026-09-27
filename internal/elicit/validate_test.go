package elicit

import (
	"encoding/json"
	"maps"
	"strings"
	"testing"
)

func everythingQuestion(t *testing.T) Question {
	t.Helper()
	q, err := fromMap(t, everythingForm())
	if err != nil {
		t.Fatalf("FromSchema: %v", err)
	}
	return q
}

func questionOf(t *testing.T, properties map[string]any) Question {
	t.Helper()
	q, err := fromMap(t, map[string]any{"type": "object", "properties": properties})
	if err != nil {
		t.Fatalf("FromSchema: %v", err)
	}
	return q
}

func TestValidateAcceptsAFullAnswer(t *testing.T) {
	t.Parallel()
	content := map[string]any{
		"name": "Ada Lovelace", "check": true, "firstLine": "Call me Ishmael.",
		"email": "ada@example.com", "homepage": "https://example.com/ada", "birthdate": "1815-12-10",
		"integer": float64(36), "number": 7.5, "untitledSingleSelectEnum": "Ross",
		"untitledMultipleSelectEnum": []any{"Piano", "Bass"}, "titledSingleSelectEnum": "hero-3",
		"titledMultipleSelectEnum": []any{"fish-2"}, "legacyTitledEnum": "pet-2",
	}
	if errs := Validate(everythingQuestion(t), content); errs != nil {
		t.Fatalf("Validate = %v, want nil", errs)
	}
}

func TestValidateGivesEachFailingFieldACode(t *testing.T) {
	t.Parallel()
	errs := Validate(everythingQuestion(t), map[string]any{
		"email": "not-an-address", "homepage": "example.com", "birthdate": "10/12/1815",
		"integer": float64(200), "number": "seven", "check": "yes",
		"untitledSingleSelectEnum":   "Janice",
		"untitledMultipleSelectEnum": []any{"Guitar", "Piano", "Violin", "Drums"},
		"titledMultipleSelectEnum":   []any{}, "firstLine": strings.Repeat("c", MaxAnswerBytes+1),
		"stranger": "x",
	})
	want := FieldErrors{
		"": ProblemNotAsked, "name": ProblemRequired, "email": ProblemFormat,
		"homepage": ProblemFormat, "birthdate": ProblemFormat, "integer": ProblemOutOfRange,
		"number": ProblemInvalid, "check": ProblemInvalid,
		"untitledSingleSelectEnum":   ProblemNotAnOption,
		"untitledMultipleSelectEnum": ProblemTooMany,
		"titledMultipleSelectEnum":   ProblemTooFew, "firstLine": ProblemTooLong,
	}
	if !maps.Equal(errs, want) {
		t.Fatalf("Validate = %v, want %v", errs, want)
	}
	if !strings.Contains(errs.Error(), "integer: out_of_range") {
		t.Fatalf("Error() = %q", errs.Error())
	}
}

func TestValidateChecksLengthsAndPatternsInCharacters(t *testing.T) {
	t.Parallel()
	q := questionOf(t, map[string]any{
		"pin":  map[string]any{"type": "string", "minLength": 4, "maxLength": 4, "pattern": "^[0-9]+$"},
		"nick": map[string]any{"type": "string", "maxLength": 3},
	})
	for value, want := range map[string]string{"123": ProblemTooShort, "12345": ProblemTooLong, "12a4": ProblemPattern} {
		if got := Validate(q, map[string]any{"pin": value})["pin"]; got != want {
			t.Fatalf("pin %q = %q, want %q", value, got, want)
		}
	}
	if errs := Validate(q, map[string]any{"pin": "1234", "nick": "ñoë"}); errs != nil {
		t.Fatalf("three characters in six bytes refused: %v", errs)
	}
}

func TestValidateChecksDateTimeAndHostlessURIs(t *testing.T) {
	t.Parallel()
	q := questionOf(t, map[string]any{
		"at":   map[string]any{"type": "string", "format": "date-time"},
		"link": map[string]any{"type": "string", "format": "uri"},
	})
	if errs := Validate(q, map[string]any{"at": "2026-09-25T08:00:00.000Z", "link": "file:///tmp/a"}); errs != nil {
		t.Fatalf("valid date-time and hostless URI refused: %v", errs)
	}
	if got := Validate(q, map[string]any{"at": "2026-09-25 08:00"})["at"]; got != ProblemFormat {
		t.Fatalf("invalid date-time = %q", got)
	}
}

func TestAProblemNeverQuotesTheAnswer(t *testing.T) {
	t.Parallel()
	const secret = "sk-live-0123456789abcdef"
	q := questionOf(t, map[string]any{
		"token": map[string]any{"type": "string", "maxLength": 8},
		"code":  map[string]any{"type": "string", "enum": []any{"a", "b"}},
		"key":   map[string]any{"type": "string", "pattern": "^[a-z]+$"},
	})
	errs := Validate(q, map[string]any{"token": secret, "code": secret, "key": secret, secret: secret})
	encoded, err := json.Marshal(errs)
	if err != nil {
		t.Fatal(err)
	}
	if len(errs) != 4 || strings.Contains(string(encoded), secret) {
		t.Fatalf("refusal %s quotes answer or misses field", encoded)
	}
	codes := []string{ProblemRequired, ProblemNotAsked, ProblemInvalid, ProblemTooShort, ProblemTooLong, ProblemOutOfRange, ProblemNotAnOption, ProblemTooFew, ProblemTooMany, ProblemFormat, ProblemPattern}
	for name, problem := range errs {
		if !strings.Contains(strings.Join(codes, " "), problem) {
			t.Fatalf("%q carries non-code %q", name, problem)
		}
	}
}

func TestValidateAMessageOnlyForm(t *testing.T) {
	t.Parallel()
	q, err := FromSchema("s", "t", "Confirm?", nil)
	if err != nil {
		t.Fatal(err)
	}
	if errs := Validate(q, nil); errs != nil {
		t.Fatalf("empty answer = %v", errs)
	}
	if got := Validate(q, map[string]any{"x": float64(1)})[""]; got != ProblemNotAsked {
		t.Fatalf("unasked content = %q", got)
	}
}

func TestValidateLetsTheLibraryCatchWhatTheProjectionSkips(t *testing.T) {
	t.Parallel()
	q := questionOf(t, map[string]any{"n": map[string]any{"type": "number", "multipleOf": 5}})
	if got := Validate(q, map[string]any{"n": float64(7)})[""]; got != ProblemInvalid {
		t.Fatalf("7 against multipleOf 5 = %q", got)
	}
}
