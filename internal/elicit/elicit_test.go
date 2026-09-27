package elicit

import (
	"context"
	"testing"
)

type nopAsker struct{}

func (nopAsker) Ask(context.Context, Question) (Answer, error) {
	return Answer{Action: ActionDecline}, nil
}

func TestAskerRidesTheContext(t *testing.T) {
	t.Parallel()
	if AskerFrom(context.Background()) != nil {
		t.Fatal("a bare context carries no asker")
	}
	var asker Asker = nopAsker{}
	if got := AskerFrom(WithAsker(context.Background(), asker)); got != asker {
		t.Fatalf("AskerFrom = %v, want the installed asker", got)
	}
}
