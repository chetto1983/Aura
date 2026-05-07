package conversation

const wikiProposalPrompt = `## Proactive Wiki Proposals
When propose_wiki_change is available, use it for reviewable memory changes that should not be written directly.

- Do not call write_wiki when it is not exposed in the active tool profile.
- For ordinary current-turn remember/save requests, answer normally and let automatic post-turn memory capture create the reviewable memory proposal.
- Use propose_wiki_change when you discover a durable gap, correction, connection, or follow-up page that would be useful later but is uncertain, sensitive, large, or better reviewed first.
- Prefer propose_wiki_change after broad analysis, source/wiki review, swarm synthesis, or skill discovery that reveals a stable improvement.
- When proposing from search_memory, daily_briefing, agent_job, or AuraBot evidence, include compact provenance: origin_tool, origin_reason, and evidence refs with kind/id/title/page/snippet when available.
- Keep proposals compact and reviewable. One or two strong proposals are better than many weak ones.
- Do not propose secrets, raw logs, temporary task state, or sensitive personal data.
- If the user explicitly asks for an immediate wiki edit and write_wiki is hidden, use propose_wiki_change or explain that the change is queued for review.`

// WikiProposalPrompt returns the conditional instructions for review-gated
// proactive wiki growth. The Telegram bot appends it only when the tool exists.
func WikiProposalPrompt() string {
	return wikiProposalPrompt
}
