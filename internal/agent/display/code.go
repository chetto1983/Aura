package display

// CodeInput is the structured value a code-producing tool (sandbox_exec /
// shell_exec) hands the normalizer. It is the SAME shape the live path holds and
// the replay path re-derives from the persisted preview (Pitfall 4): Body is the
// command output text, Lang the highlight hint, and the optional Artifact* fields
// name a single file the run produced.
type CodeInput struct {
	Body             string
	Lang             string
	ArtifactFilename string
	ArtifactSize     int64
	ArtifactPath     string
}

// normalizeCode maps a sandbox/shell result to a code Payload (text output) or a
// local_artifact Payload when the run's primary product is a named file. A run with
// neither body nor artifact is still recognized as an empty code display — the tool
// ran, the typed card just shows no output.
func normalizeCode(toolCallID string, in CodeInput) (Payload, bool) {
	if in.ArtifactFilename != "" {
		return Payload{
			Type:       KindLocalArtifact,
			ToolCallID: toolCallID,
			Title:      in.ArtifactFilename,
			Artifact: &Artifact{
				Filename:  in.ArtifactFilename,
				SizeBytes: in.ArtifactSize,
				Path:      in.ArtifactPath,
			},
		}, true
	}
	return Payload{
		Type:       KindCode,
		ToolCallID: toolCallID,
		Code:       &Code{Body: in.Body, Lang: in.Lang},
	}, true
}
