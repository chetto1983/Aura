package main

import (
	"fmt"

	"example.test/fixture/iface"
	"example.test/fixture/lib"
)

func Exported() {}

func main() {
	lib.Used()
	s := &lib.Store{}
	s.Called()
	byValue := s.ByValue
	byValue()
	(*lib.Store).ByExpression(s)
	fmt.Println(s, lib.Thing{})
	_ = iface.Use(lib.Outer{Inner: &lib.Inner{}})
	b := &lib.Box[int]{}
	_ = b.Get()
}
