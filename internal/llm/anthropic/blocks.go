package anthropic

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/chetto1983/aura/internal/llm"
)

// noOutput stands in for an empty tool result: the API refuses a blank text block.
const noOutput = "(no output)"

type wireMessage struct {
	user   bool
	blocks []anthropic.BetaContentBlockParamUnion
}

// blockRef addresses one block of the wire conversation; message -1 is none.
type blockRef struct{ message, block int }

// conversation is Aura's history in Claude's shape. ends[i] is the last wire block that
// messages[0..i] produced, which is where a breakpoint for "the first i+1 messages" goes:
// system messages are hoisted, blank text is dropped and same-role neighbours merge, so a
// message's own position says nothing about the wire.
type conversation struct {
	system   []anthropic.BetaTextBlockParam
	messages []wireMessage
	ends     []blockRef
	replayed int
}

func convert(req llm.Request, media requestMedia) (conversation, error) {
	conv := conversation{ends: make([]blockRef, len(req.Messages))}
	last := blockRef{message: -1}
	target, err := uploadTarget(req, len(media.uploads) > 0)
	if err != nil {
		return conv, err
	}
	// A call's media go only with the last result that answers it: they are this turn's,
	// and a fallback call id built from name, arguments and position can recur across turns.
	answer := map[string]int{}
	for i, message := range req.Messages {
		if message.Role == llm.RoleTool {
			answer[message.ToolCallID] = i
		}
	}
	for i, message := range req.Messages {
		var blocks []anthropic.BetaContentBlockParamUnion
		user := true
		switch message.Role {
		case llm.RoleSystem:
			if strings.TrimSpace(message.Content) != "" {
				conv.system = append(conv.system, anthropic.BetaTextBlockParam{Text: message.Content})
			}
		case llm.RoleUser:
			blocks = textBlocks(message.Content)
			if i == target {
				blocks = append(blocks, media.uploads...)
			}
		case llm.RoleAssistant:
			user = false
			replayed, ok := replayState(message)
			if ok {
				blocks = replayed
				conv.replayed++
				break
			}
			if blocks, err = assistantBlocks(message); err != nil {
				return conv, err
			}
		case llm.RoleTool:
			var shown []anthropic.BetaToolResultBlockParamContentUnion
			if answer[message.ToolCallID] == i {
				shown = media.tools[message.ToolCallID]
			}
			block, err := toolResult(message, shown)
			if err != nil {
				return conv, err
			}
			blocks = []anthropic.BetaContentBlockParamUnion{block}
		default:
			return conv, fmt.Errorf("anthropic: unsupported message role %q", message.Role)
		}
		if len(blocks) > 0 {
			last = conv.append(user, blocks)
		}
		conv.ends[i] = last
	}
	return conv, nil
}

// append merges consecutive same-role messages by concatenating their blocks, and
// returns where the last block landed.
func (c *conversation) append(user bool, blocks []anthropic.BetaContentBlockParamUnion) blockRef {
	if n := len(c.messages); n > 0 && c.messages[n-1].user == user {
		c.messages[n-1].blocks = append(c.messages[n-1].blocks, blocks...)
	} else {
		c.messages = append(c.messages, wireMessage{user: user, blocks: blocks})
	}
	n := len(c.messages) - 1
	return blockRef{message: n, block: len(c.messages[n].blocks) - 1}
}

// mark puts a breakpoint at the end of the first count messages: on the last block they
// produced that can carry one (thinking cannot). A hoisted system prompt produced no
// block, and carries its own marker.
func (c *conversation) mark(count int) {
	if count < 1 || count > len(c.ends) {
		return
	}
	c.markAt(c.ends[count-1])
}

// markHistory puts the loaded-history breakpoint on the wire message before the one that
// opens the turn. The user-role messages right before the turn's question merge with it
// into that opening message (the runner puts the turn's memory block there), and the next
// turn replays the question without them: a breakpoint among them writes an entry no later
// request reads. Measured on the lab VM, conversation 01a2223e, 2026-11-28: every new turn
// read only tools + system and wrote its whole history again.
func (c *conversation) markHistory(count int) {
	if count < 1 || count > len(c.ends) {
		return
	}
	ref := c.ends[count-1]
	if count < len(c.ends) && ref.message >= 0 && c.messages[ref.message].user && c.ends[count].message == ref.message {
		if ref.message == 0 {
			return
		}
		previous := ref.message - 1
		ref = blockRef{message: previous, block: len(c.messages[previous].blocks) - 1}
	}
	c.markAt(ref)
}

func (c *conversation) markAt(ref blockRef) {
	if ref.message < 0 {
		return
	}
	blocks := c.messages[ref.message].blocks
	for i := ref.block; i >= 0; i-- {
		if control := blocks[i].GetCacheControl(); control != nil {
			*control = ephemeral
			return
		}
	}
}

func (c *conversation) params() []anthropic.BetaMessageParam {
	out := make([]anthropic.BetaMessageParam, 0, len(c.messages))
	for _, message := range c.messages {
		if message.user {
			out = append(out, anthropic.BetaMessageParam{OfUser: &anthropic.BetaUserMessageParam{Content: message.blocks}})
		} else {
			out = append(out, anthropic.BetaMessageParam{OfAssistant: &anthropic.BetaAssistantMessageParam{Content: message.blocks}})
		}
	}
	return out
}

func textBlocks(text string) []anthropic.BetaContentBlockParamUnion {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return []anthropic.BetaContentBlockParamUnion{{OfText: &anthropic.BetaTextBlockParam{Text: text}}}
}

func assistantBlocks(message llm.Message) ([]anthropic.BetaContentBlockParamUnion, error) {
	blocks := textBlocks(message.Content)
	for _, call := range message.ToolCalls {
		if call.ID == "" || call.Function.Name == "" {
			return nil, errors.New("anthropic: tool history requires call ID and function name")
		}
		blocks = append(blocks, anthropic.BetaContentBlockParamUnion{OfToolUse: &anthropic.BetaToolUseBlockParam{
			ID: wireToolID(call.ID), Name: call.Function.Name, Input: objectInput(call.Function.Arguments),
		}})
	}
	return blocks, nil
}

// replayState returns the assistant response's own blocks, in order, with each tool_use
// input taken from the message's calls. When the calls are not exactly the state's
// tool_use ids (Aura deduped, renamed or dropped a call) the state is ignored and the
// message is rebuilt without its thinking (plan decision 2).
func replayState(message llm.Message) ([]anthropic.BetaContentBlockParamUnion, bool) {
	if len(message.ProviderState) == 0 {
		return nil, false
	}
	var blocks []anthropic.BetaContentBlockParamUnion
	if err := json.Unmarshal(message.ProviderState, &blocks); err != nil {
		return nil, false
	}
	calls := message.ToolCalls
	out := blocks[:0]
	next := 0
	for _, block := range blocks {
		if text := block.OfText; text != nil && strings.TrimSpace(text.Text) == "" {
			continue
		}
		if use := block.OfToolUse; use != nil {
			if next >= len(calls) || calls[next].ID != use.ID || calls[next].Function.Name == "" {
				return nil, false
			}
			use.ID, use.Name, use.Input = wireToolID(use.ID), calls[next].Function.Name, objectInput(calls[next].Function.Arguments)
			next++
		}
		out = append(out, block)
	}
	return out, next == len(calls)
}

func toolResult(message llm.Message, media []anthropic.BetaToolResultBlockParamContentUnion) (anthropic.BetaContentBlockParamUnion, error) {
	if message.ToolCallID == "" {
		return anthropic.BetaContentBlockParamUnion{}, errors.New("anthropic: tool result requires call ID")
	}
	text := message.Content
	if strings.TrimSpace(text) == "" {
		text = noOutput
	}
	content := append([]anthropic.BetaToolResultBlockParamContentUnion{{OfText: &anthropic.BetaTextBlockParam{Text: text}}}, media...)
	return anthropic.BetaContentBlockParamUnion{OfToolResult: &anthropic.BetaToolResultBlockParam{ToolUseID: wireToolID(message.ToolCallID), Content: content}}, nil
}

// objectInput is a call's arguments as tool_use input, which the API requires to be a
// JSON object: anything else goes as {}.
func objectInput(arguments string) json.RawMessage {
	raw := bytes.TrimSpace([]byte(arguments))
	var object map[string]json.RawMessage
	if len(raw) == 0 || raw[0] != '{' || json.Unmarshal(raw, &object) != nil {
		return json.RawMessage(`{}`)
	}
	return raw
}

var validToolID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

var invalidToolIDRune = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

// wireToolID maps an id onto the API's ^[a-zA-Z0-9_-]{1,64}$. A valid id passes byte for
// byte; any other gets a digest of the original, so a.b and a:b never collide and the
// tool_use and its tool_result always agree.
func wireToolID(id string) string {
	if validToolID.MatchString(id) {
		return id
	}
	sum := sha256.Sum256([]byte(id))
	prefix := invalidToolIDRune.ReplaceAllString(id, "_")
	if len(prefix) > 47 {
		prefix = prefix[:47]
	}
	return prefix + "_" + hex.EncodeToString(sum[:8])
}
