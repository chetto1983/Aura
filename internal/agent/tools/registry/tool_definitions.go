package tools

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
				Description: "Find all known lessons about web_search failures.",
				Arguments: map[string]any{
					"tool_name": "web_search",
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

func (t *CreateDOCXTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters:  t.Parameters(),
		Examples: []ToolCallExample{
			{
				Description: "Create a Word document from typed blocks.",
				Arguments: map[string]any{
					"filename": "riepilogo-aura.docx",
					"title":    "Riepilogo Aura",
					"blocks": []any{
						map[string]any{"kind": "heading", "level": 1, "text": "Riepilogo Aura"},
						map[string]any{"kind": "paragraph", "text": "Sintesi breve basata sulle evidenze recuperate."},
					},
				},
			},
		},
	}
}

func (t *CreateXLSXTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters:  t.Parameters(),
		Examples: []ToolCallExample{
			{
				Description: "Create a spreadsheet from rows.",
				Arguments: map[string]any{
					"filename": "riepilogo-aura.xlsx",
					"sheets": []any{
						map[string]any{
							"name": "Sintesi",
							"rows": []any{
								[]any{"Voce", "Valore"},
								[]any{"Fonti", "3"},
							},
						},
					},
				},
			},
		},
	}
}

func (t *CreatePDFTool) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters:  t.Parameters(),
		Examples: []ToolCallExample{
			{
				Description: "Create a PDF from typed blocks.",
				Arguments: map[string]any{
					"filename": "riepilogo-aura.pdf",
					"title":    "Riepilogo Aura",
					"blocks": []any{
						map[string]any{"kind": "heading", "level": 1, "text": "Riepilogo Aura"},
						map[string]any{"kind": "paragraph", "text": "Sintesi breve."},
					},
				},
			},
		},
	}
}
