package pimwatch

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// These read what the PIM fork really writes (testdata/pim_contract.json). One event the client
// could not decode would fail its whole page, and a page that fails is asked again unchanged.

func otherShapes(t *testing.T) map[string]Change {
	t.Helper()
	var page ChangesPage
	if err := json.Unmarshal(loadContract(t).OtherShapes, &page); err != nil {
		t.Fatalf("a shape the fork writes does not decode: %v", err)
	}
	byID := make(map[string]Change, len(page.Changes))
	for _, change := range page.Changes {
		byID[change.ID] = change
	}
	return byID
}

func TestAMailKeepsItsAddressAndItsNameApart(t *testing.T) {
	t.Parallel()
	var page ChangesPage
	if err := json.Unmarshal(loadContract(t).ChangesResponse, &page); err != nil {
		t.Fatal(err)
	}
	mail := page.Changes[0]
	if mail.From != "mario@acme.it" || mail.FromName != "Mario Rossi" {
		t.Fatalf("from %q, name %q", mail.From, mail.FromName)
	}
	if address, domain := SenderKey(mail.From); address != "mario@acme.it" || domain != "acme.it" {
		t.Fatalf("key = %q, %q", address, domain)
	}
}

func TestAChangeCarriesOnlyWhatItsTypeWrites(t *testing.T) {
	t.Parallel()
	shapes := otherShapes(t)
	for _, id := range []string{"m-9", "e-3"} {
		if change := shapes[id]; change.Type != ChangeDeleted || change.Event != nil || change.Subject != "" {
			t.Errorf("%s = %+v", id, change)
		}
	}
	if change := shapes["m-8"]; change.Type != ChangeUpdated || change.Event != nil {
		t.Errorf("an updated mail = %+v", change)
	}
	if mail := shapes["m-7"]; mail.From != "" || mail.FromName == "" || mail.Signals.WrittenTo != nil {
		t.Errorf("a sender with no address = %+v", mail)
	}
}

func TestACancelledGoogleInstanceHasAStartAndNoEnd(t *testing.T) {
	t.Parallel()
	event := otherShapes(t)["e-2"].Event
	if event == nil || event.Status != EventCancelled || event.Start.IsZero() || !event.End.IsZero() || event.MyResponse != "" {
		t.Fatalf("event = %+v", event)
	}
}

func TestAnEventTimeReadsWhatTheForkWrites(t *testing.T) {
	t.Parallel()
	instant := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	day := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		raw     string
		want    time.Time
		allDay  bool
		invalid bool
	}{
		{raw: `"2026-11-02T09:00:00Z"`, want: instant},
		{raw: `"2026-11-02T10:00:00+01:00"`, want: instant},
		{raw: `"2026-11-03"`, want: day, allDay: true},
		{raw: `null`},
		{raw: `"tomorrow"`, invalid: true},
		{raw: `1234`, invalid: true},
	}
	for _, tc := range cases {
		var got EventTime
		err := json.Unmarshal([]byte(tc.raw), &got)
		if tc.invalid {
			if err == nil {
				t.Errorf("%s decoded", tc.raw)
			}
			continue
		}
		if err != nil || !got.Equal(tc.want) || got.AllDay != tc.allDay {
			t.Errorf("%s = %v, all day %v, %v", tc.raw, got.Time, got.AllDay, err)
		}
	}
}

func TestAnAllDayEventStartsAtThePersonsMidnight(t *testing.T) {
	t.Parallel()
	rome, err := time.LoadLocation("Europe/Rome")
	if err != nil {
		t.Fatal(err)
	}
	start := otherShapes(t)["e-5"].Event.Start
	if want := time.Date(2026, 11, 3, 0, 0, 0, 0, rome); !start.AllDay || !start.In(rome).Equal(want) {
		t.Fatalf("start = %v in Rome, want %v", start.In(rome), want)
	}
	instant := EventTime{Time: time.Date(2026, 11, 3, 8, 0, 0, 0, time.UTC)}
	if !instant.In(rome).Equal(instant.Time) {
		t.Fatalf("an instant moved: %v", instant.In(rome))
	}
	if !(EventTime{}).In(rome).IsZero() {
		t.Fatal("a null time stopped being zero")
	}
}

// occurredAt on an event read by id is when it was last modified, and a digest that showed it
// would give the person the wrong day.
func TestAnEventReadByIDShowsItsStart(t *testing.T) {
	t.Parallel()
	var result ItemsResult
	if err := json.Unmarshal(loadContract(t).CalendarItems, &result); err != nil {
		t.Fatal(err)
	}
	items := map[string]Item{result.Items[0].ID: result.Items[0]}
	line := lineOf(Verdict{Resource: ResourceCalendar, ItemID: "e-1", CalendarID: "primary"}, items)
	if want := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC); !line.Start.Equal(want) || line.AllDay {
		t.Fatalf("start = %v, want %v", line.Start, want)
	}
	if len(result.Gone) != 1 || result.Gone[0].CalendarID != "primary" {
		t.Fatalf("gone = %+v", result.Gone)
	}
}

func TestAMailReadByIDCarriesTheName(t *testing.T) {
	t.Parallel()
	var result ItemsResult
	if err := json.Unmarshal(loadContract(t).ItemsResponse, &result); err != nil {
		t.Fatal(err)
	}
	line := lineOf(Verdict{Resource: ResourceMail, ItemID: "m-1"}, map[string]Item{"m-1": result.Items[0]})
	if line.From != "mario@acme.it" || line.FromName != "Mario Rossi" || !line.Start.IsZero() {
		t.Fatalf("line = %+v", line)
	}
	if len(result.Gone) != 1 || result.Gone[0].ID != "m-2" || result.Gone[0].CalendarID != "" {
		t.Fatalf("gone = %+v", result.Gone)
	}
}

// A PIM 5xx is transient unless it names itself otherwise, and everything else needs the person.
// The PIM leaves an exception it did not recognise as a bare 500 on purpose.
func TestEveryFailureThePIMAnswersIsClassified(t *testing.T) {
	t.Parallel()
	problem := `{"type":"https://tools.ietf.org/html/rfc9110#section-15.6.1","title":"An error occurred","status":500}`
	cases := []struct {
		status int
		body   string
		class  FailureClass
		code   string
	}{
		{400, `{"error":"invalid_cursor"}`, FailureHard, "invalid_cursor"},
		{400, `{"error":"invalid_account"}`, FailureHard, "invalid_account"},
		{400, `{"error":"invalid_resource"}`, FailureHard, "invalid_resource"},
		{400, `{"error":"invalid_items"}`, FailureHard, "invalid_items"},
		{400, `{"error":"too_many_items"}`, FailureHard, "too_many_items"},
		{404, `{"error":"account_not_found"}`, FailureHard, "account_not_found"},
		{500, problem, FailureTransient, "pim_http_500"},
		{500, ``, FailureTransient, "pim_http_500"},
		{502, `<html>bad gateway</html>`, FailureTransient, "pim_http_502"},
		{504, ``, FailureTransient, "pim_http_504"},
		{400, `{"error":"something new"}`, FailureHard, "pim_http_400"},
		{401, `{"error":"OAuth bearer token required."}`, FailureHard, "pim_http_401"},
		{403, `{"error":"OAuth token lacks the scope."}`, FailureHard, "pim_http_403"},
		{404, ``, FailureHard, "pim_http_404"},
		{204, ``, FailureHard, "pim_http_204"},
	}
	for _, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = io.WriteString(w, tc.body)
		}))
		_, err := NewClient(srv.URL, &staticToken{token: "t"}).Changes(t.Context(), "a", ResourceMail, "c", 1)
		srv.Close()
		class, code := failureClass(err)
		if class != tc.class || code != tc.code || strings.Contains(err.Error(), "OAuth") {
			t.Errorf("%d %s = class %d code %q, want %d %q", tc.status, tc.body, class, code, tc.class, tc.code)
		}
	}
}
