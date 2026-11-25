package lib

import "fmt"

func Used() {}

func Unused() {}

func TestOnly() {}

func helper() {}

type Store struct{}

func (s *Store) Called() {}

func (s *Store) Dead() {}

func (s *Store) ByValue() {}

func (s *Store) ByExpression() {}

func (s *Store) String() string { return fmt.Sprint("store") }

// Thing reaches an interface (fmt.Stringer through Println), which is exactly what keeps
// Spare alive for deadcode.
type Thing struct{}

func (Thing) String() string { return "thing" }

func (Thing) Spare() {}

// Inner's Callback is reached only by promotion: Outer embeds it and implements
// iface.Callbacker (the chatgptplan browserAuthorizer shape).
type Inner struct{}

func (*Inner) Callback() error { return nil }

type Outer struct{ *Inner }

type Box[T any] struct{ v T }

func (b *Box[T]) Get() T { return b.v }

func (b *Box[T]) Drop() {}

// TagOnly is called only from a file behind the extra build tag.
func TagOnly() {}
