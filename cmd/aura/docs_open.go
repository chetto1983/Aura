package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/identityctx"
)

// docsOpen materializes an indexed document back onto the host filesystem.
//
// The agent's document_open writes INSIDE the sandbox box, because that is the only
// filesystem the agent can then read from. This one is the host arm of the same motion and
// has the mirror-image constraint: its caller is a CLI or an MCP client on the host, for
// which the box is exactly the place nobody can look. It writes under the SAME workspace
// root `docs ingest` reads from, so the two directions cannot disagree about where the
// operator's documents live, and a file that comes out can be put back in unchanged.
func docsOpen(ctx context.Context, args []string, out io.Writer, factory docsServiceFactory) error {
	fs := flag.NewFlagSet("docs open", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fileName := fs.String("file-name", "", "name for the written copy (bare name, no directories)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("docs open requires <document-id>")
	}
	documentID := strings.TrimSpace(fs.Arg(0))
	if documentID == "" {
		return fmt.Errorf("docs open requires <document-id>")
	}
	if err := documents.ValidateStagedFileName(*fileName); err != nil {
		return fmt.Errorf("docs open: %w", err)
	}

	svc, closeFn, err := factory(ctx)
	if err != nil {
		return err
	}
	defer closeFn()
	root := strings.TrimSpace(svc.WorkspaceRoot())
	if root == "" {
		return fmt.Errorf("docs open: workspace root is not configured")
	}

	start := time.Now()
	body, meta, err := svc.OpenDocument(ctx, identityctx.IdentityID(ctx), documentID)
	if err != nil {
		return err
	}
	defer func() { _ = body.Close() }()

	path, err := openedDocumentPath(root, documentID, meta, *fileName)
	if err != nil {
		return err
	}
	size, digest, err := writeOpenedDocument(path, body, meta.SizeBytes)
	if err != nil {
		return fmt.Errorf("docs open: %w", err)
	}
	return writeJSON(out, map[string]any{
		"path":        path,
		"file_name":   filepath.Base(path),
		"mime_type":   meta.MIMEType,
		"size_bytes":  size,
		"sha256":      digest,
		"document_id": meta.DocumentID,
		"source_key":  meta.SourceKey,
		// The index's own digest, reported beside the one just measured off the bytes so a
		// caller can see they agree instead of taking the claim on faith.
		"indexed_sha256": meta.SHA256,
		"open_ms":        time.Since(start).Milliseconds(),
	})
}

// openedDocumentPath places the copy under the shared per-document layout.
//
// The record's own name goes through the same rule as the caller's: it becomes a path
// component, so a separator in it would let the join walk out of the document's directory.
func openedDocumentPath(root, requestedID string, meta documents.OpenedDocument, alias string) (string, error) {
	name := strings.TrimSpace(alias)
	if name == "" {
		name = strings.TrimSpace(meta.FileName)
	}
	// The index answers with its own id; falling back to the requested one keeps a record
	// that carries none from failing on a layout rule rather than on the thing that is wrong.
	stagedID := strings.TrimSpace(meta.DocumentID)
	if stagedID == "" {
		stagedID = requestedID
	}
	relative, err := documents.StagedRelativePath(stagedID, name)
	if err != nil {
		return "", fmt.Errorf("docs open: document %s: %w", requestedID, err)
	}
	return filepath.Join(root, filepath.FromSlash(relative)), nil
}

// writeOpenedDocument streams body to path and returns what actually landed there.
//
// Nothing is reported until the whole stream has arrived. The bytes go to a sibling
// temporary file that is renamed only on success, and a failed copy takes its partial
// with it: a truncated spreadsheet that looks like a whole one is worse than no file,
// because the answer computed from it is confident and wrong. When the record declares a
// size, a stream that delivers any other count is a failure, not a shorter document.
func writeOpenedDocument(path string, body io.Reader, declared int64) (int64, string, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return 0, "", fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	partial := path + ".partial"
	// #nosec G304 -- partial is not caller-controlled by the time it reaches here: the root
	// comes from the loaded config, the directory is a hash of the document id, and the only
	// caller-influenced component is the file name, which StagedRelativePath has already
	// refused unless it is a single, non-hidden path component.
	file, err := os.OpenFile(partial, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, "", fmt.Errorf("create %s: %w", partial, err)
	}
	digest := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(file, digest), body)
	closeErr := file.Close()
	switch {
	case copyErr != nil:
		err = fmt.Errorf("write %s: %w", path, copyErr)
	case closeErr != nil:
		err = fmt.Errorf("write %s: %w", path, closeErr)
	case declared > 0 && size != declared:
		err = fmt.Errorf("write %s: got %d bytes, the index declares %d", path, size, declared)
	}
	if err != nil {
		_ = os.Remove(partial)
		return 0, "", err
	}
	if err := os.Rename(partial, path); err != nil {
		_ = os.Remove(partial)
		return 0, "", fmt.Errorf("write %s: %w", path, err)
	}
	return size, hex.EncodeToString(digest.Sum(nil)), nil
}
