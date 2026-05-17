package stringx

import (
	"reflect"
	"testing"
)

func TestAppendUnique_AppendsNewValues(t *testing.T) {
	got := AppendUnique([]string{"a", "b"}, "c", "d")
	want := []string{"a", "b", "c", "d"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestAppendUnique_SkipsDuplicates(t *testing.T) {
	got := AppendUnique([]string{"a", "b"}, "b", "a", "c")
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestAppendUnique_SkipsEmptyAndWhitespace(t *testing.T) {
	got := AppendUnique(nil, "", "  ", "\t", "x")
	want := []string{"x"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestAppendUnique_TrimsAdditions(t *testing.T) {
	got := AppendUnique([]string{"a"}, "  b  ", "a  ")
	want := []string{"a", "b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestAppendUnique_CaseSensitive(t *testing.T) {
	got := AppendUnique([]string{"a"}, "A")
	want := []string{"a", "A"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestAppendUnique_NilValuesIsOK(t *testing.T) {
	got := AppendUnique(nil, "a", "b")
	want := []string{"a", "b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
