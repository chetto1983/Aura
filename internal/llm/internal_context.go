package llm

// InternalContext is provider-neutral historical data. Authoritative must remain false.
type InternalContext struct {
	Role          string
	Content       string
	Authoritative bool
}
