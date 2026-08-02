package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/google/uuid"
)

type adaptiveBenchmarkMemoryFixture struct {
	nodeType string
	alias    string
	category string
	value    string
}

var adaptiveBenchmarkMemoryFixtures = []adaptiveBenchmarkMemoryFixture{
	{
		nodeType: "preference", alias: "preference:temperature-unit",
		category: "temperature-unit",
		value:    "Use Celsius for every temperature.",
	},
	{
		nodeType: "entity", alias: "fact:answer-style",
		value: "Benchmark answer style is concise.",
	},
	{
		nodeType: "entity", alias: "fact:workshop-location",
		value: "Benchmark workshop location is Turin Bay 3.",
	},
	{
		nodeType: "preference", alias: "preference:notification-channel",
		category: "notification-channel",
		value:    "Send benchmark notifications by email.",
	},
	{
		nodeType: "entity", alias: "fact:maintenance-day",
		value: "Benchmark maintenance day is Tuesday.",
	},
	{
		nodeType: "preference", alias: "preference:timezone",
		category: "timezone",
		value:    "Use Europe Rome timezone for benchmark scheduling.",
	},
	{
		nodeType: "preference", alias: "preference:measurement-system",
		category: "measurement-system",
		value:    "Use the metric measurement system.",
	},
	{
		nodeType: "entity", alias: "fact:project-codename",
		value: "Benchmark project codename is Helios.",
	},
	{
		nodeType: "preference", alias: "preference:language",
		category: "language",
		value:    "Use Italian for benchmark replies.",
	},
	{
		nodeType: "preference", alias: "preference:response-length",
		category: "response-length",
		value:    "Keep benchmark responses concise.",
	},
	{
		nodeType: "entity", alias: "fact:emergency-contact-alias",
		value: "Benchmark emergency contact alias is Nora Ops.",
	},
}

func (fixtures *adaptiveBenchmarkFixtureSet) seedMemory(
	ctx context.Context,
	ownerID uuid.UUID,
	runID uuid.UUID,
) ([]adaptiveBenchmarkFixtureAliasEntry, error) {
	entries := make(
		[]adaptiveBenchmarkFixtureAliasEntry,
		0,
		len(adaptiveBenchmarkMemoryFixtures),
	)
	for _, fixture := range adaptiveBenchmarkMemoryFixtures {
		metadata := map[string]any{
			"run_id":          runID.String(),
			"scope":           "adaptive-benchmark",
			"benchmark_alias": fixture.alias,
			"synthetic":       true,
		}
		tool := "memory_add_entity"
		args := map[string]any{
			"name":            fixture.value + " [" + runID.String() + "]",
			"entity_type":     "OBJECT",
			"description":     fixture.value,
			"metadata":        metadata,
			"user_identifier": ownerID.String(),
		}
		if fixture.nodeType == "preference" {
			tool = "memory_add_preference"
			args = map[string]any{
				"category":   fixture.category,
				"preference": fixture.value,
				"context": "Synthetic adaptive benchmark fixture " +
					runID.String(),
				"metadata":        metadata,
				"user_identifier": ownerID.String(),
			}
		}
		response, err := fixtures.memory.CallTool(ctx, tool, args)
		if err != nil {
			return nil, fmt.Errorf(
				"seed adaptive benchmark memory %s: %w",
				fixture.alias,
				err,
			)
		}
		id, err := adaptiveBenchmarkFixtureResponseID(response)
		if err != nil {
			return nil, fmt.Errorf(
				"seed adaptive benchmark memory %s: %w",
				fixture.alias,
				err,
			)
		}
		fixtures.memoryNodes = append(
			fixtures.memoryNodes,
			adaptiveBenchmarkFixtureMemoryNode{
				nodeType: fixture.nodeType,
				id:       id,
			},
		)
		entries = append(entries, adaptiveBenchmarkFixtureAliasEntry{
			domain: adaptive.DomainMemoryRecall,
			id:     id,
			alias:  fixture.alias,
		})
	}
	return entries, nil
}

func adaptiveBenchmarkFixtureResponseID(payload string) (uuid.UUID, error) {
	decoded, err := decodeAdaptiveBenchmarkFixtureJSON(payload)
	if err != nil {
		return uuid.Nil, err
	}
	var idText string
	if err := json.Unmarshal(decoded["id"], &idText); err != nil {
		return uuid.Nil, errors.New(
			"adaptive benchmark fixture response omits ID",
		)
	}
	id, err := uuid.Parse(idText)
	if err != nil || id == uuid.Nil || id.String() != idText {
		return uuid.Nil, errors.New(
			"adaptive benchmark fixture response ID is invalid",
		)
	}
	return id, nil
}

func (fixtures *adaptiveBenchmarkFixtureSet) cleanupMemory(
	ctx context.Context,
) error {
	var cleanupErr error
	for _, v := range slices.Backward(fixtures.memoryNodes) {
		node := v
		if err := fixtures.forgetMemory(ctx, node, true); err != nil {
			cleanupErr = errors.Join(cleanupErr, err)
			continue
		}
		if err := fixtures.forgetMemory(ctx, node, false); err != nil {
			cleanupErr = errors.Join(cleanupErr, err)
		}
	}
	return cleanupErr
}

func (fixtures *adaptiveBenchmarkFixtureSet) forgetMemory(
	ctx context.Context,
	node adaptiveBenchmarkFixtureMemoryNode,
	mustDelete bool,
) error {
	response, err := fixtures.memory.CallTool(
		ctx,
		"memory_forget",
		map[string]any{
			"node_type":       node.nodeType,
			"node_id":         node.id.String(),
			"user_identifier": fixtures.ownerID.String(),
		},
	)
	if err != nil {
		return err
	}
	decoded, err := decodeAdaptiveBenchmarkFixtureJSON(response)
	if err != nil {
		return err
	}
	var deleted *string
	if err := json.Unmarshal(decoded["deleted"], &deleted); err != nil {
		return errors.New(
			"decode adaptive benchmark memory cleanup result",
		)
	}
	if mustDelete && (deleted == nil || *deleted != node.id.String()) {
		return errors.New(
			"adaptive benchmark memory fixture was not deleted",
		)
	}
	if !mustDelete && deleted != nil {
		return errors.New(
			"adaptive benchmark memory fixture survived cleanup",
		)
	}
	return nil
}

func validAdaptiveBenchmarkFixtureAlias(alias string) bool {
	return alias != "" &&
		strings.TrimSpace(alias) == alias &&
		!strings.ContainsAny(alias, "\r\n\t")
}
