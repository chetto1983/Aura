package pimwatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type staticToken struct {
	token string
	err   error
	asked []string
}

func (s *staticToken) AccessToken(_ context.Context, server string) (string, error) {
	s.asked = append(s.asked, server)
	return s.token, s.err
}

// contract is testdata/pim_contract.json: the requests and answers spec §5.1 fixes. The fake PIM
// here answers from it, and the pim_contract_live tier holds the real sidecar to the same file.
type contract struct {
	ChangesRequest   json.RawMessage `json:"changesRequest"`
	ChangesResponse  json.RawMessage `json:"changesResponse"`
	OtherShapes      json.RawMessage `json:"otherShapesResponse"`
	ItemsRequest     json.RawMessage `json:"itemsRequest"`
	ItemsResponse    json.RawMessage `json:"itemsResponse"`
	CalendarItems    json.RawMessage `json:"calendarItemsResponse"`
	EmptyCursorAsks  json.RawMessage `json:"emptyCursorRequest"`
	EmptyCursorGives json.RawMessage `json:"emptyCursorResponse"`
	Failures         []struct {
		Status int    `json:"status"`
		Body   string `json:"body"`
	} `json:"failures"`
}

func loadContract(t *testing.T) contract {
	t.Helper()
	raw, err := os.ReadFile("testdata/pim_contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var c contract
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestClientReadsTheContractsChanges(t *testing.T) {
	t.Parallel()
	c := loadContract(t)
	var gotAuth, gotType string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotType = r.Header.Get("Authorization"), r.Header.Get("Content-Type")
		if r.Method != http.MethodPost || r.URL.Path != "/admin/changes" || r.URL.RawQuery != "" {
			t.Errorf("request = %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write(c.ChangesResponse)
	}))
	defer srv.Close()
	tokens := &staticToken{token: "tok"}

	page, err := NewClient(srv.URL+"/", tokens).Changes(t.Context(), "work", ResourceMail, "cur-1", 25)
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}
	if gotAuth != "Bearer tok" || gotType != "application/json" || tokens.asked[0] != PIMServer {
		t.Fatalf("auth %q, type %q, server %v", gotAuth, gotType, tokens.asked)
	}
	var want map[string]any
	if err := json.Unmarshal(c.ChangesRequest, &want); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(gotBody) != fmt.Sprint(want) {
		t.Fatalf("body = %v, want %v", gotBody, want)
	}
	if page.NextCursor == "" || !page.HasMore || len(page.Changes) != 2 {
		t.Fatalf("page = %+v", page)
	}
	mail := page.Changes[0]
	if mail.Type != ChangeAdded || mail.From == "" || mail.Subject == "" || mail.Signals.WrittenTo == nil ||
		!mail.Signals.ProviderImportant || !mail.Signals.Invitation {
		t.Fatalf("mail change = %+v", mail)
	}
	event := page.Changes[1]
	if event.CalendarID == "" || event.Event == nil || event.Event.Fingerprint == "" || event.Event.Start.IsZero() {
		t.Fatalf("event change = %+v", event)
	}
}

func TestClientReadsTheContractsItems(t *testing.T) {
	t.Parallel()
	c := loadContract(t)
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/items" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write(c.ItemsResponse)
	}))
	defer srv.Close()

	got, err := NewClient(srv.URL, &staticToken{token: "t"}).Items(t.Context(), "work", ResourceMail, []ItemRef{{ID: "m-1"}, {ID: "m-2"}})
	if err != nil {
		t.Fatalf("Items: %v", err)
	}
	var want map[string]any
	if err := json.Unmarshal(c.ItemsRequest, &want); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(gotBody) != fmt.Sprint(want) {
		t.Fatalf("body = %v, want %v", gotBody, want)
	}
	if len(got.Items) != 1 || got.Items[0].From == "" || len(got.Gone) != 1 || got.Gone[0].ID != "m-2" {
		t.Fatalf("result = %+v", got)
	}
}

func TestItemsBatchesAtTheContractsBound(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var batches []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Items []ItemRef `json:"items"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		batches = append(batches, len(body.Items))
		mu.Unlock()
		items := make([]Item, 0, len(body.Items))
		for _, ref := range body.Items {
			items = append(items, Item{ID: ref.ID})
		}
		_ = json.NewEncoder(w).Encode(ItemsResult{Items: items})
	}))
	defer srv.Close()
	refs := make([]ItemRef, 120)
	for i := range refs {
		refs[i] = ItemRef{ID: fmt.Sprintf("m-%d", i)}
	}

	got, err := NewClient(srv.URL, &staticToken{token: "t"}).Items(t.Context(), "work", ResourceMail, refs)
	if err != nil {
		t.Fatalf("Items: %v", err)
	}
	if len(got.Items) != 120 || fmt.Sprint(batches) != "[50 50 20]" {
		t.Fatalf("items %d, batches %v", len(got.Items), batches)
	}
}

func TestClientNamesEveryContractFailure(t *testing.T) {
	t.Parallel()
	cases := []struct {
		status int
		body   string
		class  FailureClass
		code   string
	}{
		{503, `{"error":"provider_transient"}`, FailureTransient, "provider_transient"},
		{502, `{"error":"provider_auth"}`, FailureHard, "provider_auth"},
		{403, `{"error":"permission_denied"}`, FailureHard, "permission_denied"},
		{422, `{"error":"unsupported_resource"}`, FailureUnsupported, "unsupported_resource"},
		{500, `boom`, FailureTransient, "pim_http_500"},
		{429, ``, FailureTransient, "pim_http_429"},
		{404, `{"error":"account_not_found"}`, FailureHard, "account_not_found"},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()
			_, err := NewClient(srv.URL, &staticToken{token: "t"}).Changes(t.Context(), "a", ResourceMail, "c", 1)
			class, code := failureClass(err)
			if class != tc.class || code != tc.code {
				t.Fatalf("class %d code %q, want %d %q", class, code, tc.class, tc.code)
			}
		})
	}
}

func TestTheContractsFailuresAreAllNamed(t *testing.T) {
	t.Parallel()
	for _, f := range loadContract(t).Failures {
		if err := classifyStatus(f.Status, []byte(f.Body)); strings.HasPrefix(err.Code, "pim_http_") {
			t.Errorf("contract failure %d %s has no name here", f.Status, f.Body)
		}
	}
}

// A failure must not carry the PIM's text: it could name a provider URL, and this error reaches
// the run ledger and a cursor row.
func TestAFailureCarriesNoResponseText(t *testing.T) {
	t.Parallel()
	const secret = "https://graph.microsoft.com/v1.0/delta?$deltatoken=SECRET"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, secret, http.StatusBadGateway)
	}))
	defer srv.Close()
	_, err := NewClient(srv.URL, &staticToken{token: "t"}).Changes(t.Context(), "a", ResourceMail, "cursor-SECRET", 1)
	if err == nil || strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), srv.URL) {
		t.Fatalf("error = %v", err)
	}
}

func TestAnUnreachablePIMIsTransientAndNamesNoHost(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	_, err := NewClient(url, &staticToken{token: "t"}).Changes(t.Context(), "a", ResourceMail, "c", 1)
	class, code := failureClass(err)
	if class != FailureTransient || code != "pim_unreachable" || strings.Contains(err.Error(), url) {
		t.Fatalf("class %d code %q err %v", class, code, err)
	}
}

func TestAnEmptyNextCursorIsNeverFollowed(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"changes":[],"nextCursor":""}`)
	}))
	defer srv.Close()
	_, err := NewClient(srv.URL, &staticToken{token: "t"}).Changes(t.Context(), "a", ResourceMail, "c", 1)
	if class, code := failureClass(err); class != FailureTransient || code != "pim_empty_cursor" {
		t.Fatalf("class %d code %q", class, code)
	}
}

func TestABadBodyIsTransient(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `<html>`)
	}))
	defer srv.Close()
	_, err := NewClient(srv.URL, &staticToken{token: "t"}).Changes(t.Context(), "a", ResourceMail, "c", 1)
	if class, code := failureClass(err); class != FailureTransient || code != "pim_bad_response" {
		t.Fatalf("class %d code %q", class, code)
	}
}

func TestTokenFailures(t *testing.T) {
	t.Parallel()
	cases := []struct {
		err   error
		class FailureClass
		code  string
	}{
		{fmt.Errorf("wrapped: %w", ErrNotAuthorized), FailureHard, "pim_not_authorized"},
		{errors.New("refresh timed out"), FailureTransient, "pim_token_unavailable"},
	}
	for _, tc := range cases {
		client := NewClient("http://pim.invalid", &staticToken{err: tc.err})
		_, err := client.Changes(t.Context(), "a", ResourceMail, "c", 1)
		if class, code := failureClass(err); class != tc.class || code != tc.code {
			t.Errorf("%v: class %d code %q", tc.err, class, code)
		}
	}
}

func TestACanceledContextIsTransient(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	_, err := NewClient(srv.URL, &staticToken{token: "t"}).Changes(ctx, "a", ResourceMail, "c", 1)
	if class, _ := failureClass(err); class != FailureTransient {
		t.Fatalf("class = %d", class)
	}
}

func TestFailureClassOfAnOrdinaryError(t *testing.T) {
	t.Parallel()
	if class, code := failureClass(errors.New("x")); class != FailureTransient || code != "pim_call_failed" {
		t.Fatalf("class %d code %q", class, code)
	}
}

func TestNewClientNeedsAURLAndATokenSource(t *testing.T) {
	t.Parallel()
	if NewClient("", &staticToken{}) != nil || NewClient("  ", &staticToken{}) != nil {
		t.Fatal("a client with no sidecar URL")
	}
	if NewClient("http://pim", nil) != nil {
		t.Fatal("a client with no token source")
	}
}

func TestEventTimesDecode(t *testing.T) {
	t.Parallel()
	var page ChangesPage
	if err := json.Unmarshal(loadContract(t).ChangesResponse, &page); err != nil {
		t.Fatal(err)
	}
	if page.Changes[1].Event.Start.Before(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("start = %v", page.Changes[1].Event.Start)
	}
}
