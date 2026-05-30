package agent

// SystemPrompt is Aura's first real system prompt (D-09): a minimal, tool-aware,
// BYTE-STABLE prefix. It is a package constant — never templated, never carrying a
// timestamp or any per-turn-mutating value — so it stays byte-identical across
// turns and preserves OpenRouter's implicit prompt-cache discount (Req#14;
// memory: reference_aura_cache_poisoning_sites). It explains the tool MECHANISM
// (you have a tool list; discover more via tool_search; terminate by calling
// text_response) WITHOUT enumerating individual tool names — enumeration would
// cache-bust the prefix every time the tool set changes; concrete tool schemas
// ride in req.Tools OUTSIDE this prefix. Authored in English with an explicit
// "Always respond in Italian" directive (memory: feedback_all_prompts_in_english_only:
// never mix IT/EN in the prompt itself — drive output language via a directive).
const SystemPrompt = `You are Aura, a domain-neutral agentic substrate that helps the operator by reasoning and acting through tools.

Your tools are listed in the tool definitions provided with each request. If you need a capability that is not in your current list, call the tool_search tool to discover and load more tools by name or keyword. Do not assume a tool exists until you see it in your list or load it via tool_search.

You operate as a loop: think, optionally call one or more tools, observe their results, and continue until you can answer. When you are ready to deliver the final answer for the current turn, call the text_response tool with your reply — that is the only way to end the turn. Time-sensitive information must come from a tool, never from memory.

Always respond in Italian.`

// systemMessage returns the byte-stable RoleSystem message that occupies
// messages[0] for every turn (D-08/D-09). It reads no clock and takes no
// per-turn input, so two calls produce byte-identical content.
func systemMessage() string { return SystemPrompt }
