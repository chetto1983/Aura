package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/google/uuid"
)

type adaptiveBenchmarkDocumentFixture struct {
	alias   string
	title   string
	content string
}

var adaptiveBenchmarkDocumentFixtures = []adaptiveBenchmarkDocumentFixture{
	{
		alias:   "document:project-helios",
		title:   "Project Helios launch record",
		content: "The private launch code for Project Helios is kestrel-47.",
	},
	{
		alias:   "document:orion-x",
		title:   "Orion-X warranty",
		content: "The Orion-X inverter warranty period is 7 years.",
	},
	{
		alias:   "document:northstar",
		title:   "Northstar vendor contact",
		content: "The contact alias for vendor Northstar is nora-ops.",
	},
	{
		alias:   "document:warehouse-delta",
		title:   "Warehouse Delta emergency radio",
		content: "Warehouse Delta uses emergency radio channel 6.",
	},
	{
		alias:   "document:pump-atlas",
		title:   "Pump Atlas maintenance",
		content: "The maintenance window for Pump Atlas is Tuesday 02:00 UTC.",
	},
	{
		alias:   "document:project-ember",
		title:   "Project Ember supply plan",
		content: "The supplier lead time for Project Ember is 18 days.",
	},
	{
		alias:   "document:device-kappa",
		title:   "Device Kappa credential policy",
		content: "Credentials for Device Kappa must rotate every 90 days.",
	},
	{
		alias:   "document:robot-mica",
		title:   "Robot Mica safety classification",
		content: "Robot Mica has safety class S3.",
	},
}

func (fixtures *adaptiveBenchmarkFixtureSet) seedDocuments(
	ctx context.Context,
	ownerID uuid.UUID,
	runID uuid.UUID,
) ([]adaptiveBenchmarkFixtureAliasEntry, error) {
	indexer := &documents.Indexer{Client: fixtures.graph}
	entries := make(
		[]adaptiveBenchmarkFixtureAliasEntry,
		0,
		len(adaptiveBenchmarkDocumentFixtures),
	)
	for _, fixture := range adaptiveBenchmarkDocumentFixtures {
		documentID := uuid.NewSHA1(
			runID,
			[]byte("document\x00"+fixture.alias),
		)
		chunkID := uuid.NewSHA1(
			runID,
			[]byte("chunk\x00"+fixture.alias),
		)
		fixtures.documentIDs = append(
			fixtures.documentIDs,
			documentID.String(),
		)
		fixtures.chunkIDs = append(fixtures.chunkIDs, chunkID.String())
		contentSum := sha256.Sum256([]byte(fixture.content))
		contentSHA256 := hex.EncodeToString(contentSum[:])
		document := documents.ExtractedDocument{
			ID:          documentID.String(),
			SourceID:    runID.String(),
			SourceKind:  "adaptive-benchmark",
			FileName:    fixture.alias + ".txt",
			MIMEType:    "text/plain",
			SizeBytes:   int64(len(fixture.content)),
			ContentHash: contentSHA256,
			Title:       fixture.title,
			IdentityID:  ownerID.String(),
			Chunks: []documents.Chunk{{
				ID:          chunkID.String(),
				DocumentID:  documentID.String(),
				SourceID:    runID.String(),
				ContentHash: contentSHA256,
				ChunkHash:   contentSHA256,
				ChunkIndex:  0,
				ChunkCount:  1,
				Kind:        "text",
				Text:        fixture.content,
				Locator:     documents.Locator{Section: fixture.alias},
				HeadingPath: []string{fixture.title},
			}},
		}
		if _, err := indexer.UpsertSparse(ctx, document); err != nil {
			return nil, fmt.Errorf(
				"seed adaptive benchmark document %s: %w",
				fixture.alias,
				err,
			)
		}
		entries = append(entries, adaptiveBenchmarkFixtureAliasEntry{
			domain: adaptive.DomainKnowledge,
			id:     chunkID,
			alias:  fixture.alias,
		})
	}
	return entries, nil
}

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

func (fixtures *adaptiveBenchmarkFixtureSet) cleanupDocuments(
	ctx context.Context,
) error {
	var cleanupErr error
	if len(fixtures.chunkIDs) != 0 {
		_, err := fixtures.graph.Write(
			ctx,
			`MATCH (c:Chunk) WHERE c.id IN $ids DETACH DELETE c`,
			map[string]any{"ids": slices.Clone(fixtures.chunkIDs)},
		)
		cleanupErr = errors.Join(cleanupErr, err)
	}
	if len(fixtures.documentIDs) != 0 {
		_, err := fixtures.graph.Write(
			ctx,
			`MATCH (d:Document) WHERE d.id IN $ids DETACH DELETE d`,
			map[string]any{"ids": slices.Clone(fixtures.documentIDs)},
		)
		cleanupErr = errors.Join(cleanupErr, err)
	}
	rows, err := fixtures.graph.Read(
		ctx,
		`MATCH (n) WHERE (n:Document OR n:Chunk) AND n.id IN $ids `+
			`RETURN count(n) AS remaining`,
		map[string]any{
			"ids": append(
				slices.Clone(fixtures.documentIDs),
				fixtures.chunkIDs...,
			),
		},
	)
	if err != nil {
		return errors.Join(cleanupErr, err)
	}
	if len(rows) != 1 || adaptiveBenchmarkFixtureCount(
		rows[0]["remaining"],
	) != 0 {
		cleanupErr = errors.Join(
			cleanupErr,
			errors.New(
				"adaptive benchmark document fixtures survived cleanup",
			),
		)
	}
	return cleanupErr
}

func adaptiveBenchmarkFixtureCount(value any) int64 {
	switch count := value.(type) {
	case int:
		return int64(count)
	case int32:
		return int64(count)
	case int64:
		return count
	case float64:
		return int64(count)
	default:
		return -1
	}
}

func validAdaptiveBenchmarkFixtureAlias(alias string) bool {
	return alias != "" &&
		strings.TrimSpace(alias) == alias &&
		!strings.ContainsAny(alias, "\r\n\t")
}
