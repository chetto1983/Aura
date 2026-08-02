package runner

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

func TestDynamicRecallCatalogUsesExactConfiguredThresholds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		enabled bool
		max     int
		want    []DynamicRecallAction
	}{
		{name: "disabled", max: 8, want: []DynamicRecallAction{DynamicRecallStatic}},
		{name: "below four", enabled: true, max: 3, want: []DynamicRecallAction{DynamicRecallStatic}},
		{name: "top four", enabled: true, max: 4, want: []DynamicRecallAction{
			DynamicRecallStatic, DynamicRecallTop4,
		}},
		{name: "between thresholds", enabled: true, max: 7, want: []DynamicRecallAction{
			DynamicRecallStatic, DynamicRecallTop4,
		}},
		{name: "top eight", enabled: true, max: 8, want: []DynamicRecallAction{
			DynamicRecallStatic, DynamicRecallTop4, DynamicRecallTop8,
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := DynamicRecallCatalog(test.enabled, test.max)
			if !slices.Equal(got, test.want) {
				t.Fatalf("catalog = %v, want %v", got, test.want)
			}
			for _, action := range got {
				if action == "off" || action == DynamicRecallAction(agent.DynamicTailFallbackContextBudget) {
					t.Fatalf("catalog contains forbidden event outcome %q", action)
				}
			}
		})
	}
	if got := FenceDynamicRecall(" "); got != "" {
		t.Fatalf("blank fenced recall = %q", got)
	}
	if dynamicRecallActionAllowed(
		DynamicRecallTop4,
		[]DynamicRecallAction{DynamicRecallStatic},
	) {
		t.Fatal("disallowed recall action was accepted")
	}
}

func TestPrepareDynamicRecallUsesTypedLongTermProviderOnce(t *testing.T) {
	t.Parallel()
	r, _, _ := newTestRunner(t, nil)
	convID := newConvID(t)
	mustCreate(t, r, convID)
	r.recallMaxItems = 8
	order := []string{}
	providerCalls := 0
	r.dynamicRecallProvider = func(
		_ context.Context,
		userIdentifier string,
		query string,
		maxItems int,
	) (DynamicRecall, error) {
		order = append(order, "provider")
		providerCalls++
		if userIdentifier != "00000000-0000-0000-0000-000000000001" {
			t.Fatalf("user identifier = %q", userIdentifier)
		}
		if query != "current question" || maxItems != 4 {
			t.Fatalf("provider query/max = (%q,%d), want current question/4", query, maxItems)
		}
		return validDynamicRecall(), nil
	}
	r.dynamicRecallControl = dynamicRecallControlFunc(func(
		ctx context.Context,
		input DynamicRecallInput,
		provider DynamicRecallProvider,
	) (*PreparedDynamicRecall, error) {
		order = append(order, "assignment")
		if !slices.Equal(input.CandidateActions, []DynamicRecallAction{
			DynamicRecallStatic, DynamicRecallTop4, DynamicRecallTop8,
		}) {
			t.Fatalf("candidate actions = %v", input.CandidateActions)
		}
		recall, err := provider(ctx, input.OwnerID.String(), input.Query, 4)
		if err != nil {
			return nil, err
		}
		return &PreparedDynamicRecall{
			Action: DynamicRecallTop4,
			Recall: recall,
			Commit: func(context.Context, agent.DynamicTailOutcome) error {
				return nil
			},
		}, nil
	})

	prepared := r.prepareDynamicRecall(
		context.Background(),
		convID,
		uuid.Must(uuid.NewV7()),
		"current question",
		true,
	)

	if prepared == nil {
		t.Fatal("prepared dynamic recall = nil")
	}
	if !slices.Equal(order, []string{"assignment", "provider"}) {
		t.Fatalf("order = %v, want assignment before provider", order)
	}
	if providerCalls != 1 {
		t.Fatalf("provider calls = %d, want one", providerCalls)
	}
	if prepared.exposure.Tail.Content != validDynamicRecall().Text {
		t.Fatal("prepared tail did not preserve fenced bytes")
	}
}

func TestPrepareDynamicRecallFailsClosedOnMetadataOrProviderFailure(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		provider DynamicRecallProvider
	}{
		{name: "provider failure", provider: func(
			context.Context, string, string, int,
		) (DynamicRecall, error) {
			return DynamicRecall{}, errors.New("memory unavailable")
		}},
		{name: "incoherent epoch", provider: func(
			context.Context, string, string, int,
		) (DynamicRecall, error) {
			recall := validDynamicRecall()
			*recall.CorpusEpochAfter = *recall.CorpusEpochBefore + 1
			return recall, nil
		}},
		{name: "missing ordered results", provider: func(
			context.Context, string, string, int,
		) (DynamicRecall, error) {
			recall := validDynamicRecall()
			recall.Results = nil
			recall.Limits = map[string]DynamicRecallLimit{}
			return recall, nil
		}},
		{name: "non long-term result kind", provider: func(
			context.Context, string, string, int,
		) (DynamicRecall, error) {
			recall := validDynamicRecall()
			recall.Results[0].Kind = "memory_reasoning_trace"
			delete(recall.Limits, "memory_preference")
			recall.Limits["memory_reasoning_trace"] = DynamicRecallLimit{
				RequestedK: 4, EffectiveK: 4, Count: 1,
			}
			return recall, nil
		}},
		{name: "missing long-term limit", provider: func(
			context.Context, string, string, int,
		) (DynamicRecall, error) {
			recall := validDynamicRecall()
			delete(recall.Limits, "memory_entity")
			return recall, nil
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			r, _, _ := newTestRunner(t, nil)
			convID := newConvID(t)
			mustCreate(t, r, convID)
			r.recallMaxItems = 8
			r.dynamicRecallProvider = test.provider
			r.dynamicRecallControl = dynamicRecallControlFunc(func(
				ctx context.Context,
				_ DynamicRecallInput,
				provider DynamicRecallProvider,
			) (*PreparedDynamicRecall, error) {
				recall, err := provider(ctx, "00000000-0000-0000-0000-000000000001", "query", 4)
				if err != nil {
					return nil, err
				}
				return &PreparedDynamicRecall{
					Action: DynamicRecallTop4,
					Recall: recall,
					Commit: func(context.Context, agent.DynamicTailOutcome) error {
						return nil
					},
				}, nil
			})

			if got := r.prepareDynamicRecall(
				context.Background(), convID, uuid.Must(uuid.NewV7()), "query", true,
			); got != nil {
				t.Fatalf("prepared = %#v, want static fallback", got)
			}
		})
	}
}

func TestDynamicRecallRunnerWiringCoversFreshAndBranchPlacement(t *testing.T) {
	tests := []struct {
		name       string
		run        func(*Runner, string) ([]*agent.Event, error)
		wantQuery  string
		wantBefore string
		wantTail   bool
	}{
		{
			name: "fresh",
			run: func(r *Runner, conversationID string) ([]*agent.Event, error) {
				return drain(r.Turn(
					context.Background(),
					conversationID,
					new("current question"),
				))
			},
			wantQuery:  "current question",
			wantBefore: "current question",
		},
		{
			name: "branch",
			run: func(r *Runner, conversationID string) ([]*agent.Event, error) {
				return drain(r.TurnBranch(
					context.Background(),
					conversationID,
					7,
				))
			},
			wantTail: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			order := []string{}
			script := agenttest.NewFakeClient(
				agenttest.ToolCallTurn(agenttest.MakeToolCall(
					"probe-1",
					"request_identity_probe",
					`{}`,
				)),
				agenttest.ToolCallTurn(textResponseCall("done-1", "done")),
			)
			client := &dynamicRecallRunnerClient{
				delegate: script,
				order:    &order,
			}
			r, conv, _ := newTestRunner(t, client)
			r.registry.Register(&requestIdentityTool{})
			conversationID := newConvID(t)
			mustCreate(t, r, conversationID)
			conv.mu.Lock()
			conv.convs[conversationID].TitleSet = true
			conv.mu.Unlock()
			var providerCalls int
			r.recallMaxItems = 8
			r.dynamicRecallProvider = func(
				context.Context,
				string,
				string,
				int,
			) (DynamicRecall, error) {
				providerCalls++
				order = append(order, "provider")
				return validDynamicRecall(), nil
			}
			r.dynamicRecallControl = dynamicRecallControlFunc(func(
				ctx context.Context,
				input DynamicRecallInput,
				provider DynamicRecallProvider,
			) (*PreparedDynamicRecall, error) {
				if input.Query != test.wantQuery {
					t.Fatalf("recall query = %q, want %q", input.Query, test.wantQuery)
				}
				order = append(order, "assignment")
				recall, err := provider(
					ctx,
					input.OwnerID.String(),
					input.Query,
					4,
				)
				return &PreparedDynamicRecall{
					Action: DynamicRecallTop4,
					Recall: recall,
					Commit: func(
						context.Context,
						agent.DynamicTailOutcome,
					) error {
						order = append(order, "delivery")
						return nil
					},
				}, err
			})

			if _, err := test.run(r, conversationID); err != nil {
				t.Fatalf("turn: %v", err)
			}
			if providerCalls != 1 {
				t.Fatalf("provider calls = %d, want 1", providerCalls)
			}
			if !slices.Equal(
				order,
				[]string{"assignment", "provider", "delivery", "model", "model"},
			) {
				t.Fatalf("wiring order = %v", order)
			}
			for round, request := range script.Requests {
				index := messageIndexForRecall(
					request.Messages,
					validDynamicRecall().Text,
				)
				if index < 0 {
					t.Fatalf("round %d omitted dynamic recall", round)
				}
				if test.wantBefore != "" &&
					(index+1 >= len(request.Messages) ||
						request.Messages[index+1].Content != test.wantBefore) {
					t.Fatalf(
						"round %d recall not before current user: %#v",
						round,
						request.Messages,
					)
				}
				if test.wantTail && index != len(request.Messages)-1 {
					t.Fatalf(
						"round %d branch recall not at tail: %#v",
						round,
						request.Messages,
					)
				}
			}
			conv.mu.Lock()
			persisted := append(
				[]conversations.AppendTurnParams(nil),
				conv.turns[conversationID]...,
			)
			conv.mu.Unlock()
			for _, turn := range persisted {
				if turn.Content == validDynamicRecall().Text {
					t.Fatal("dynamic recall was persisted")
				}
			}
		})
	}
}

func TestPrepareDynamicRecallFinalizesStaticAndInvalidAssignments(t *testing.T) {
	tests := []struct {
		name   string
		action DynamicRecallAction
		recall DynamicRecall
		want   agent.DynamicTailOutcome
	}{
		{
			name:   "static",
			action: DynamicRecallStatic,
			want:   agent.DynamicTailOutcome{},
		},
		{
			name:   "invalid learned response",
			action: DynamicRecallTop4,
			recall: DynamicRecall{Text: "unfenced"},
			want: agent.DynamicTailOutcome{
				FallbackReason: agent.DynamicTailFallbackInvalid,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r, _, _ := newTestRunner(t, nil)
			conversationID := newConvID(t)
			mustCreate(t, r, conversationID)
			r.recallMaxItems = 8
			r.dynamicRecallProvider = func(
				context.Context,
				string,
				string,
				int,
			) (DynamicRecall, error) {
				t.Fatal("provider must be controlled by the assignment adapter")
				return DynamicRecall{}, nil
			}
			var outcomes []agent.DynamicTailOutcome
			r.dynamicRecallControl = dynamicRecallControlFunc(func(
				context.Context,
				DynamicRecallInput,
				DynamicRecallProvider,
			) (*PreparedDynamicRecall, error) {
				return &PreparedDynamicRecall{
					Action: test.action,
					Recall: test.recall,
					Commit: func(
						_ context.Context,
						outcome agent.DynamicTailOutcome,
					) error {
						outcomes = append(outcomes, outcome)
						return nil
					},
				}, nil
			})

			if got := r.prepareDynamicRecall(
				context.Background(),
				conversationID,
				uuid.Must(uuid.NewV7()),
				"query",
				true,
			); got != nil {
				t.Fatalf("prepared = %#v, want static path", got)
			}
			if !slices.Equal(outcomes, []agent.DynamicTailOutcome{test.want}) {
				t.Fatalf("outcomes = %#v, want %#v", outcomes, test.want)
			}
		})
	}
}

func TestPrepareDynamicRecallFailsSoftBeforeAssignmentExposure(t *testing.T) {
	controlError := errors.New("decision unavailable")
	tests := []struct {
		name    string
		control DynamicRecallControl
	}{
		{name: "nil control"},
		{
			name: "control error",
			control: dynamicRecallControlFunc(func(
				context.Context,
				DynamicRecallInput,
				DynamicRecallProvider,
			) (*PreparedDynamicRecall, error) {
				return nil, controlError
			}),
		},
		{
			name: "nil prepared",
			control: dynamicRecallControlFunc(func(
				context.Context,
				DynamicRecallInput,
				DynamicRecallProvider,
			) (*PreparedDynamicRecall, error) {
				return nil, nil
			}),
		},
		{
			name: "missing delivery port",
			control: dynamicRecallControlFunc(func(
				context.Context,
				DynamicRecallInput,
				DynamicRecallProvider,
			) (*PreparedDynamicRecall, error) {
				return &PreparedDynamicRecall{
					Action: DynamicRecallTop4,
					Recall: validDynamicRecall(),
				}, nil
			}),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r, _, _ := newTestRunner(t, nil)
			conversationID := newConvID(t)
			mustCreate(t, r, conversationID)
			r.recallMaxItems = 8
			r.dynamicRecallProvider = func(
				context.Context,
				string,
				string,
				int,
			) (DynamicRecall, error) {
				return validDynamicRecall(), nil
			}
			r.dynamicRecallControl = test.control
			if got := r.prepareDynamicRecall(
				context.Background(),
				conversationID,
				uuid.Must(uuid.NewV7()),
				"query",
				true,
			); got != nil {
				t.Fatalf("prepared = %#v, want fail-soft static", got)
			}
		})
	}

	r, _, _ := newTestRunner(t, nil)
	r.recallMaxItems = 8
	r.dynamicRecallProvider = func(
		context.Context,
		string,
		string,
		int,
	) (DynamicRecall, error) {
		return validDynamicRecall(), nil
	}
	r.dynamicRecallControl = tests[1].control
	if got := r.prepareDynamicRecall(
		context.Background(),
		newConvID(t),
		uuid.Must(uuid.NewV7()),
		"query",
		true,
	); got != nil {
		t.Fatalf("missing owner prepared = %#v", got)
	}
}

type dynamicRecallRunnerClient struct {
	delegate *agenttest.FakeClient
	order    *[]string
}

func (client *dynamicRecallRunnerClient) Stream(
	ctx context.Context,
	request llm.Request,
) (<-chan llm.Chunk, error) {
	*client.order = append(*client.order, "model")
	return client.delegate.Stream(ctx, request)
}

func messageIndexForRecall(messages []llm.Message, content string) int {
	for index, message := range messages {
		if message.Content == content {
			return index
		}
	}
	return -1
}

type dynamicRecallControlFunc func(
	context.Context,
	DynamicRecallInput,
	DynamicRecallProvider,
) (*PreparedDynamicRecall, error)

func (fn dynamicRecallControlFunc) PrepareDynamicRecall(
	ctx context.Context,
	input DynamicRecallInput,
	provider DynamicRecallProvider,
) (*PreparedDynamicRecall, error) {
	return fn(ctx, input, provider)
}

func validDynamicRecall() DynamicRecall {
	before := uint64(42)
	after := uint64(42)
	return DynamicRecall{
		Text: FenceDynamicRecall("exact fenced recall"),
		Results: []DynamicRecallResult{
			{
				Kind:  "memory_preference",
				ID:    "11111111-1111-4111-8111-111111111111",
				Order: 0,
			},
			{
				Kind:  "memory_entity",
				ID:    "22222222-2222-4222-8222-222222222222",
				Order: 1,
			},
		},
		Limits: map[string]DynamicRecallLimit{
			"memory_preference": {RequestedK: 4, EffectiveK: 4, Count: 1},
			"memory_entity":     {RequestedK: 4, EffectiveK: 4, Count: 1},
		},
		Revisions: DynamicRecallRevisions{
			Retriever: "arcadedb-long-term-v1",
			Reranker:  "none-v1",
			Embedding: "ollama-granite-embedding-278m@768",
			Index:     "entity-embedding-index-v1",
		},
		CorpusEpochBefore: &before,
		CorpusEpochAfter:  &after,
		Coherent:          true,
	}
}
