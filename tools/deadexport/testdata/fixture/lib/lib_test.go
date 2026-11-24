package lib

import "testing"

func TestTestOnly(t *testing.T) {
	TestOnly()
	(&Store{}).Dead()
	helper()
}
