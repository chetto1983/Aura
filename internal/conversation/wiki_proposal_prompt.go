package conversation

const wikiProposalPrompt = `## Proactive Wiki Proposals
When propose_wiki_change is available, use it to help the second brain grow without writing directly.

- Use propose_wiki_change when you discover a durable gap, correction, connection, or follow-up page that would be useful later but the user did not explicitly ask you to write now.
- Prefer propose_wiki_change after broad analysis, source/wiki review, swarm synthesis, or skill discovery that reveals a stable improvement.
- When proposing from search_memory, daily_briefing, agent_job, or AuraBot evidence, include compact provenance: origin_tool, origin_reason, and evidence refs with kind/id/title/page/snippet when available.
- Keep proposals compact and reviewable. One or two strong proposals are better than many weak ones.
- Do not propose secrets, raw logs, temporary task state, or sensitive personal data.
- If the user explicitly asks you to remember or save something, use the normal write tool when appropriate; proposals are for proactive/review-gated growth.`

// WikiProposalPrompt returns the conditional instructions for review-gated
// proactive wiki growth. The Telegram bot appends it only when the tool exists.
func WikiProposalPrompt() string {
	return wikiProposalPrompt
}
