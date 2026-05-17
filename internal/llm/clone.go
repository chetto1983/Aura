package llm

// CloneMessages returns a shallow copy of src. The slice header is fresh so
// callers can append or splice without disturbing the source, but the inner
// ToolCalls slices (when present) still share backing storage. That matches
// every existing call site — none of them mutate ToolCalls — so widening to
// a deep clone would just be wasted allocation.
//
// A nil src returns nil (the zero-value slice is not what callers usually
// want when guarding against "no messages").
func CloneMessages(src []Message) []Message {
	if src == nil {
		return nil
	}
	out := make([]Message, len(src))
	copy(out, src)
	return out
}
