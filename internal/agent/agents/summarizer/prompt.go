// Package summarizer embeds the payload-summarizer extraction prompt.
package summarizer

import _ "embed"

//go:embed SKILL.md
var Prompt string
