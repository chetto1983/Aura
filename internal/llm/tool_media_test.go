package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func imagePart(path string) ProjectedRequestPart {
	return ProjectedRequestPart{Type: "media", MIMEType: "image/png", Text: path, Bytes: []byte("png:" + path)}
}

// Tool batches run in parallel, so every call of a batch may Add at once. Under -race this
// is the proof the carrier is safe to share across the batch.
func TestToolMediaParallelAddKeepsEveryPart(t *testing.T) {
	media := &ToolMedia{}
	var wg sync.WaitGroup
	for i := range MaxToolMediaPerTurn {
		wg.Go(func() {
			if err := media.Add(fmt.Sprintf("call-%d", i), imagePart(fmt.Sprintf("/workspace/%d.png", i))); err != nil {
				t.Errorf("Add call-%d: %v", i, err)
			}
		})
	}
	wg.Wait()
	got := media.Snapshot()
	if len(got) != MaxToolMediaPerTurn {
		t.Fatalf("snapshot holds %d calls, want %d", len(got), MaxToolMediaPerTurn)
	}
	for i := range MaxToolMediaPerTurn {
		parts := got[fmt.Sprintf("call-%d", i)]
		if len(parts) != 1 || parts[0].Text != fmt.Sprintf("/workspace/%d.png", i) {
			t.Fatalf("call-%d parts = %+v", i, parts)
		}
	}
}

func TestToolMediaRefusesPastTheTurnCap(t *testing.T) {
	media := &ToolMedia{}
	for i := range MaxToolMediaPerTurn {
		if err := media.Add("call", imagePart(fmt.Sprint(i))); err != nil {
			t.Fatalf("Add %d under the cap: %v", i, err)
		}
	}
	err := media.Add("call", imagePart("one too many"))
	if !errors.Is(err, ErrToolMediaFull) {
		t.Fatalf("Add past the cap err = %v, want ErrToolMediaFull", err)
	}
	if n := len(media.Snapshot()["call"]); n != MaxToolMediaPerTurn {
		t.Fatalf("a refused Add still registered: %d parts", n)
	}
}

func TestToolMediaRefusesAPartWithNoToolCall(t *testing.T) {
	media := &ToolMedia{}
	if err := media.Add("", imagePart("/workspace/a.png")); err == nil {
		t.Fatal("Add with no tool call id succeeded; the part could never be placed after a result")
	}
	if media.Snapshot() != nil {
		t.Fatalf("snapshot = %+v, want nil", media.Snapshot())
	}
}

// The prefix of every request in the turn must stay stable, so two snapshots of the same
// carrier are identical, a call's parts keep the order they were added in, and a snapshot
// is not an alias a later Add can change behind the caller's back.
func TestToolMediaSnapshotIsDeterministicAndDetached(t *testing.T) {
	media := &ToolMedia{}
	for _, path := range []string{"/workspace/a.png", "/workspace/b.png"} {
		if err := media.Add("call-1", imagePart(path)); err != nil {
			t.Fatal(err)
		}
	}
	first, second := media.Snapshot(), media.Snapshot()
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("two snapshots differ:\n%+v\n%+v", first, second)
	}
	if got := first["call-1"]; len(got) != 2 || got[0].Text != "/workspace/a.png" || got[1].Text != "/workspace/b.png" {
		t.Fatalf("call-1 parts = %+v, want a.png then b.png", got)
	}
	if err := media.Add("call-1", imagePart("/workspace/c.png")); err != nil {
		t.Fatal(err)
	}
	if len(first["call-1"]) != 2 {
		t.Fatalf("a later Add changed an earlier snapshot: %+v", first["call-1"])
	}
}

func TestToolMediaAddCopiesTheCallersBytes(t *testing.T) {
	media := &ToolMedia{}
	raw := []byte("png")
	if err := media.Add("call", ProjectedRequestPart{MIMEType: "image/png", Bytes: raw}); err != nil {
		t.Fatal(err)
	}
	raw[0] = 'X'
	if got := string(media.Snapshot()["call"][0].Bytes); got != "png" {
		t.Fatalf("stored bytes = %q, want the bytes as they were at Add", got)
	}
}

// A command hook receives the request as JSON on its stdin; the image bytes must not ride along.
func TestRequestJSONLeavesToolMediaOut(t *testing.T) {
	part := imagePart("/workspace/a.png")
	raw, err := json.Marshal(Request{Model: "m", ToolMedia: map[string][]ProjectedRequestPart{"call": {part}}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "ToolMedia") || strings.Contains(string(raw), base64.StdEncoding.EncodeToString(part.Bytes)) {
		t.Fatalf("serialized request carries tool media: %s", raw)
	}
}

func TestToolMediaContextScoping(t *testing.T) {
	if ToolMediaFromContext(context.Background()) != nil {
		t.Fatal("a bare context carries tool media")
	}
	var nilMedia *ToolMedia
	if nilMedia.Snapshot() != nil {
		t.Fatal("a nil carrier snapshots to something")
	}
	outer, outerMedia := WithToolMedia(context.Background())
	if ToolMediaFromContext(outer) != outerMedia {
		t.Fatal("the carrier is not reachable from its own context")
	}
	inner, innerMedia := WithToolMedia(outer)
	if innerMedia == outerMedia || ToolMediaFromContext(inner) != innerMedia {
		t.Fatal("a nested run does not get its own carrier")
	}
}
