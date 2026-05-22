package tools

func (t *ProposePatchTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:           t.Name(),
		Description:    t.Description(),
		Parameters:     t.Parameters(),
		VisibilityTier: VisibilityActiveTurn,
		Examples: []ToolCallExample{
			{
				Description: "Propose a wiki page edit for operator review.",
				Arguments: map[string]any{
					"action":         "wiki",
					"target_slug":    "home",
					"body":           "# Home\nUpdated overview.",
					"change_summary": "Added updated overview section.",
				},
			},
		},
	}
}

func (t *SearchMemoryTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:          t.Name(),
		Description:   t.Description(),
		Parameters:    t.Parameters(),
		ReadOnlyHint:  true,
		OpenWorldHint: true,
		VisibilityTier: VisibilityActiveTurn,
		Examples: []ToolCallExample{
			{
				Description: "Retrieve compact evidence before answering from Aura memory.",
				Arguments: map[string]any{
					"query": "documenti e note disponibili in Aura",
					"scope": "all",
					"limit": 3,
				},
			},
		},
	}
}

func (t *RecallOperationalTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:           t.Name(),
		Description:    t.Description(),
		Parameters:     t.Parameters(),
		ReadOnlyHint:   true,
		IdempotentHint: true,
		VisibilityTier: VisibilityActiveTurn,
		Examples: []ToolCallExample{
			{
				Description: "Find all known lessons about web tool failures.",
				Arguments: map[string]any{
					"tool_name": "web",
					"limit":     5,
				},
			},
		},
	}
}

func (t *RecallUserMemoryTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:           t.Name(),
		Description:    t.Description(),
		Parameters:     t.Parameters(),
		ReadOnlyHint:   true,
		IdempotentHint: true,
		VisibilityTier: VisibilityActiveTurn,
		Examples: []ToolCallExample{
			{
				Description: "Find all preference facts about the operator.",
				Arguments: map[string]any{
					"category": "preference",
					"limit":    5,
				},
			},
		},
	}
}

func (t *AgentNoteTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:           t.Name(),
		Description:    t.Description(),
		Parameters:     t.Parameters(),
		IdempotentHint: false,
		VisibilityTier: VisibilityActiveTurn,
		Examples: []ToolCallExample{
			{
				Description: "Record a working plan for the current conversation.",
				Arguments: map[string]any{
					"action":  "set",
					"content": "TODO:\n- verify X\n- summarize Y\n- report Z",
				},
			},
		},
	}
}

