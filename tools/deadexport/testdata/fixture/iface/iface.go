// Package iface declares the interface a promoted method reaches the program through.
package iface

type Callbacker interface{ Callback() error }

func Use(c Callbacker) error { return c.Callback() }
