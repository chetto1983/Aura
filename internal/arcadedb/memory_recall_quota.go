package arcadedb

// One ranking over two record types was the wrong question, and every way of answering
// it failed the same way.
//
// `vector.rerank` re-scores the whole fused candidate set by cosine against the query
// vector (engine source SQLFunctionVectorRerank.java: it reads each candidate's embedding
// and emits VectorUtils.cosineSimilarity), so a FACT edge and a ConversationTurn vertex
// were ordered by one number in one pool. Measured 2026-09-04 on the operator's live
// memory (67 facts, 394 turns, 14 questions whose answer is a fact and 7 whose answer is
// a turn), on FOUR embedding models each in its own documented input format:
//
//	                        facts alone      one cosine pool
//	EmbeddingGemma-300M     MRR 0.964        MRR 0.643, worst rank 46
//	qwen3-embedding:0.6b    MRR 0.929        MRR 0.537, worst rank 58
//	codestral-embed-2505    MRR 0.964        MRR 0.600, worst rank 17
//	mistral-embed-2312      MRR 0.911        MRR 0.621, worst rank 73
//
// Every model is excellent on facts alone and every one loses half of it to the mixing,
// so the embedding model and its prefixes are not the cause and changing either buys
// nothing. The cause is that two documents embedded in isolation carry no common scale:
// a conversation turn is verbose prose in the operator's own words and a fact is a terse
// third-person summary, so for a question phrased by the operator the turn wins on cosine
// whatever it says.
//
// Re-weighting cannot fix it because rank fusion is a switch, not a dial. At fact weight
// 1.0 the facts score MRR 0.482 and the turns 0.810; at 1.1 the facts jump to 0.964 and
// the turns collapse to 0.118 -- both legs expose the same rank ladder, so any weight
// above one places every fact above every turn wholesale. LINEAR and DBSF normalise each
// source to a common span and behave identically for the same reason. This is the third
// independent confirmation of prd.md's own "equal-weight fusion reduced dense retrieval
// from 8/8 to 5/8 forbids assuming RRF is the winner", which the recall path had been
// contradicting.
//
// So the question is removed rather than answered, which is what GraphRAG does: give each
// evidence type a SHARE of the budget, rank it only against its own kind, and never
// compare across types (mixed_context.py, text_unit_prop / community_prop). Measured on
// the same corpus, "is the answer in the block" at the SAME five-slot budget:
//
//	one pool, limit 5, 3 turns max (shipped)   facts 0.79   turns 1.00
//	quotas 2 facts + 3 turns                   facts 1.00   turns 1.00
//	quotas 3 facts + 2 turns                   facts 1.00   turns 1.00
//
// and the optimum is a wide plateau -- anything from two to three of each at five slots,
// two to six at eight -- collapsing only when a type drops below two slots. That is why
// there is no tuned constant here: the rule is "at least two of each", not a number.
//
// What the measurement does NOT show: 21 questions on one operator's memory in one
// language, with gold sets chosen by hand for the fact side and by corpus keyword for the
// turn side. It is enough to compare two compositions against each other at equal budget,
// which is what it was built for, and it is nowhere near a retrieval benchmark -- for
// scale, the best system in graphify's LOCOMO table scores recall@10 0.497. A 1.00 here
// measures that these questions are easy, not that retrieval is solved.

// recallMinSlotsPerKind is the floor the plateau ends at. Below two slots a type stops
// being able to answer anything: at one fact slot the fact side measured 0.93 and at zero
// it measured 0.00, and the turn side falls the same way.
const recallMinSlotsPerKind = 2

// recallQuota is how many slots each evidence kind may occupy in one recall.
type recallQuota struct {
	Facts int
	Turns int
}

// recallQuotaFor splits a caller's limit between the two kinds.
//
// Facts take the larger half. Not because facts matter more -- both sides measured 1.00
// across the plateau -- but because a fact is one line and a conversation arrives as a
// window of turns, so an evenly split budget spends most of the block on the wordier
// half. Below the point where both kinds can hold their floor the split stops being
// meaningful and the limit is honoured as a single pool of facts, which is the shape a
// caller asking for one or two records is really asking for.
func recallQuotaFor(limit int) recallQuota {
	if limit < 2*recallMinSlotsPerKind {
		return recallQuota{Facts: limit}
	}
	turns := max(limit/2, recallMinSlotsPerKind)
	return recallQuota{Facts: limit - turns, Turns: turns}
}

// mergeRecallRankings concatenates the two per-kind rankings in the order evidence is
// built from. Both lists arrive whole: the quota is spent at ADMISSION, not here.
//
// Cutting each list to its quota first would under-fill the block, because hydration
// drops records after the fact -- a fact whose prose repeats an earlier one, a turn from
// an excluded or already-admitted conversation, a window left empty once fact prose is
// removed. A ceiling applied before those drops is a ceiling on candidates, and what the
// caller asked for is a count of ANSWERS.
//
// Facts lead, and that is the ONLY cross-kind ordering decision made anywhere. Within a
// kind the engine's ranking is preserved untouched and no fact score is ever compared
// with a turn score.
func mergeRecallRankings(facts, turns []recallRankedRID) []recallRankedRID {
	merged := make([]recallRankedRID, 0, len(facts)+len(turns))
	merged = append(merged, facts...)
	return append(merged, turns...)
}

// recallAdmission spends the quota as evidence is built, one kind at a time.
type recallAdmission struct {
	facts int
	turns int
}

// admitFact reports whether another fact still fits, and books the slot when it does. A
// fact is decided entirely from data already in hand, so asking and booking are one step.
func (a *recallAdmission) admitFact(quota recallQuota) bool {
	if a.facts == quota.Facts {
		return false
	}
	a.facts++
	return true
}

// canAdmitTurn and bookTurn are deliberately two calls, unlike the fact side.
//
// A conversation still has to survive its window read, which is a query and can come back
// empty once fact prose is removed. Booking before that read burns a slot on a record
// that is then dropped, so the block silently returns fewer conversations than the quota
// allows; asking only after it spends a query the quota had already refused. So: ask
// first, read, then book what actually made it.
func (a *recallAdmission) canAdmitTurn(quota recallQuota) bool {
	return a.turns < quota.Turns
}

func (a *recallAdmission) bookTurn() {
	a.turns++
}

// exhausted reports that both quotas are spent, so the ranking walk can stop.
func (a *recallAdmission) exhausted(quota recallQuota) bool {
	return a.facts == quota.Facts && a.turns == quota.Turns
}
