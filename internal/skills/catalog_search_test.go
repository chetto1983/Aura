package skills

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSkillsCatalogAPIClientSearch(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("q"); got != "go test" {
			t.Errorf("q = %q, want go test", got)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("Accept = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"query":"go test","unknown":true,"skills":[`+
			`{"source":"first/repo","skillId":"alpha","installs":153037,"future":"ignored"},`+
			`{"source":"","skillId":"drop-source","installs":9},`+
			`{"source":"drop/skill","skillId":"","installs":8},`+
			`{"source":"second/repo","skillId":"beta","installs":4638}`+
			`]}`)
	}))
	defer server.Close()

	client := newSkillsCatalogAPIClient(server.Client(), server.URL)
	hits, err := client.Search(t.Context(), "go test")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	want := []CatalogHit{
		{Source: "first/repo", Skill: "alpha", Installs: "153K"},
		{Source: "second/repo", Skill: "beta", Installs: "4.6K"},
	}
	if fmt.Sprint(hits) != fmt.Sprint(want) {
		t.Fatalf("hits = %#v, want %#v", hits, want)
	}
}

func TestSkillsCatalogAPIClientCapsResultsAtTwenty(t *testing.T) {
	t.Parallel()
	var rows []string
	for i := range 25 {
		rows = append(rows, fmt.Sprintf(
			`{"source":"owner/repo","skillId":"skill-%02d","installs":%d}`, i, i,
		))
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"skills":[%s]}`, strings.Join(rows, ","))
	}))
	defer server.Close()
	hits, err := newSkillsCatalogAPIClient(server.Client(), server.URL).Search(t.Context(), "x")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != catalogMaxHits || hits[0].Skill != "skill-00" || hits[19].Skill != "skill-19" {
		t.Fatalf("rank/cap = %#v", hits)
	}
}

func TestCompactInstallCount(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		in   int64
		want string
	}{
		{0, "0"}, {999, "999"}, {1000, "1K"}, {4638, "4.6K"},
		{12000, "12K"}, {153037, "153K"}, {1250000, "1.3M"},
	} {
		if got := compactInstallCount(tc.in); got != tc.want {
			t.Errorf("compactInstallCount(%d) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSkillsCatalogAPIClientErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"status", func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "no", http.StatusServiceUnavailable)
		}},
		{"malformed JSON", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, `{`) }},
		{"missing skills", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, `{}`) }},
		{"null skills", func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, `{"skills":null}`)
		}},
		{"wrong skills type", func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, `{"skills":{}}`)
		}},
		{"oversized", func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, `{"skills":[],"pad":"`+
				strings.Repeat("x", catalogResponseMaxBytes)+`"}`)
		}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(tc.handler)
			defer server.Close()
			_, err := newSkillsCatalogAPIClient(server.Client(), server.URL).
				Search(t.Context(), "docx")
			if err == nil {
				t.Fatal("Search error = nil")
			}
		})
	}
}

func TestSkillsCatalogAPIClientAllowsEmptySkillsArray(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"skills":[]}`)
	}))
	defer server.Close()
	hits, err := newSkillsCatalogAPIClient(server.Client(), server.URL).
		Search(t.Context(), "no-match")
	if err != nil {
		t.Fatal(err)
	}
	if hits == nil || len(hits) != 0 {
		t.Fatalf("hits = %#v, want non-nil empty slice", hits)
	}
}

func TestSkillsCatalogAPIClientHonorsCancellation(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(t.Context())
	errCh := make(chan error, 1)
	go func() {
		_, err := newSkillsCatalogAPIClient(server.Client(), server.URL).Search(ctx, "docx")
		errCh <- err
	}()
	<-started
	cancel()
	if err := <-errCh; err == nil {
		t.Fatal("Search error = nil after cancellation")
	}
}
