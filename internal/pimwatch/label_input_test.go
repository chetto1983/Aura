package pimwatch

import (
	"errors"
	"strings"
	"testing"
)

func TestLabelInputRefusesALongDescriptionAndTheNoneNames(t *testing.T) {
	if err := checkLabelInput(LabelInput{Name: "Cliente", Description: strings.Repeat("è", labelDescriptionRunes)}); err != nil {
		t.Fatalf("300 runes: %v", err)
	}
	err := checkLabelInput(LabelInput{Name: "Cliente", Description: strings.Repeat("è", labelDescriptionRunes+1)})
	var bad *InvalidError
	if !errors.As(err, &bad) || bad.Code != "invalid_label_description" {
		t.Fatalf("301 runes: %v", err)
	}
	for _, name := range []string{"nessuna", "NONE", " None "} {
		err := checkLabelInput(LabelInput{Name: name})
		if !errors.As(err, &bad) || bad.Code != "label_reserved" {
			t.Fatalf("%q: %v", name, err)
		}
	}
}

func TestNoneNameFollowsTheLanguage(t *testing.T) {
	if noneName(LangIT) != "Nessuna" || noneName(LangEN) != "None" {
		t.Fatalf("none names = %q, %q", noneName(LangIT), noneName(LangEN))
	}
}
