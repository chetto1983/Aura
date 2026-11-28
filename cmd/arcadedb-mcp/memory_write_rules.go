package main

// memoryWriteRules rides in the descriptions of both write tools, so the rules are in
// front of the model when it writes whatever compaction did to the memory skill: on
// the lab VM on 2026-11-28 the 30 KB skill holding them had left the context by turn
// #227. The skill opens with the same rules in prose; a test keeps the two aligned.
const memoryWriteRules = "Before the first write, call memory_entities once and take subject and object " +
	"from it when one fits; a new name is a noun someone would ask about again, never a " +
	"description. The predicate is English snake_case, one verb phrase per relation, reused " +
	"from the listed predicates when one already says it. The statement is one or two " +
	"sentences. Pass source: {} when there are no message ids; never invent one. Write " +
	"nothing that will not matter in a month: no record of what this conversation just did."
