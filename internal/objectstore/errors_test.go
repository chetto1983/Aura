package objectstore

import (
	"errors"
	"fmt"
	"io/fs"
	"testing"
)

func TestIsNotFound(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "filesystem", err: fmt.Errorf("head: %w", fs.ErrNotExist), want: true},
		{name: "http 404", err: statusObjectError{status: 404}, want: true},
		{name: "S3 no such key", err: codeObjectError{code: "NoSuchKey"}, want: true},
		{name: "authorization", err: statusObjectError{status: 403}},
		{name: "transport", err: errors.New("connection reset")},
		{name: "wrong S3 code", err: codeObjectError{code: "SlowDown"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := IsNotFound(test.err); got != test.want {
				t.Fatalf("IsNotFound() = %t, want %t", got, test.want)
			}
		})
	}
}

type statusObjectError struct{ status int }

func (e statusObjectError) Error() string       { return "object status error" }
func (e statusObjectError) HTTPStatusCode() int { return e.status }

type codeObjectError struct{ code string }

func (e codeObjectError) Error() string     { return "object code error" }
func (e codeObjectError) ErrorCode() string { return e.code }
