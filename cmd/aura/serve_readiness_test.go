package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/readiness"
	"github.com/goccy/go-yaml"
)

func TestComposeAuraReadinessHealthcheckContract(t *testing.T) {
	root := repoRootForTest(t)
	valid := readProjectFile(t, root, "compose.yaml")
	if err := validateAuraComposeReadiness([]byte(valid), agui.ReadinessProbeBudget); err != nil {
		t.Fatalf("production compose readiness: %v", err)
	}

	cases := map[string]string{
		"liveness endpoint":            strings.Replace(valid, "/readyz", "/healthz", 1),
		"missing max time":             strings.Replace(valid, "--max-time 3 ", "", 1),
		"external host":                strings.Replace(valid, "127.0.0.1:9080/readyz", "aura:9080/readyz", 1),
		"timeout equals server budget": strings.Replace(valid, "--max-time 3", "--max-time 2", 1),
	}
	for name, contents := range cases {
		t.Run(name, func(t *testing.T) {
			if err := validateAuraComposeReadiness([]byte(contents), agui.ReadinessProbeBudget); err == nil {
				t.Fatal("invalid Aura healthcheck passed semantic validation")
			}
		})
	}
}

func TestReadyzReflectsRuntimeLossAndRecoveryWithoutRestart(t *testing.T) {
	snapshot := readiness.NewSnapshot(readiness.Config{MigrationCompatible: true})
	snapshot.MarkListenerRunning()
	var postgresHealthy atomic.Bool
	postgresHealthy.Store(true)
	server := agui.NewServer(nil, nil, agui.ServerConfig{
		ReadinessState: snapshot,
		ReadinessProbes: []agui.ReadinessProbe{{
			Name: "postgres",
			Code: readiness.CodePostgresUnavailable,
			Check: func(context.Context) error {
				if !postgresHealthy.Load() {
					return errors.New("driver detail must stay private")
				}
				return nil
			},
		}},
	})
	httpServer := httptest.NewServer(server.Mux())
	t.Cleanup(httpServer.Close)

	assertReadyzStatus(t, httpServer.URL, http.StatusOK)
	postgresHealthy.Store(false)
	assertReadyzStatus(t, httpServer.URL, http.StatusServiceUnavailable)
	postgresHealthy.Store(true)
	assertReadyzStatus(t, httpServer.URL, http.StatusOK)

	snapshot.SetSchedulerEnabled(true)
	assertReadyzStatus(t, httpServer.URL, http.StatusServiceUnavailable)
	snapshot.MarkSchedulerProgress()
	assertReadyzStatus(t, httpServer.URL, http.StatusOK)
}

func validateAuraComposeReadiness(contents []byte, serverBudget time.Duration) error {
	type healthcheck struct {
		Test        []string `yaml:"test"`
		Interval    string   `yaml:"interval"`
		Timeout     string   `yaml:"timeout"`
		Retries     int      `yaml:"retries"`
		StartPeriod string   `yaml:"start_period"`
	}
	type service struct {
		Healthcheck healthcheck `yaml:"healthcheck"`
	}
	var document struct {
		Services map[string]service `yaml:"services"`
	}
	if err := yaml.Unmarshal(contents, &document); err != nil {
		return fmt.Errorf("parse compose: %w", err)
	}
	aura, ok := document.Services["aura"]
	if !ok {
		return errors.New("compose service aura is missing")
	}
	hc := aura.Healthcheck
	if len(hc.Test) < 2 || hc.Test[0] != "CMD-SHELL" {
		return errors.New("aura healthcheck must use an explicit CMD-SHELL command")
	}
	command := strings.Join(hc.Test[1:], " ")
	urlMatch := regexp.MustCompile(`https?://[^\s"']+`).FindString(command)
	probeURL, err := url.Parse(urlMatch)
	if err != nil || probeURL.Hostname() != "127.0.0.1" || probeURL.Path != "/readyz" {
		return fmt.Errorf("aura healthcheck must target loopback /readyz, got %q", urlMatch)
	}
	maxTimeMatch := regexp.MustCompile(`(?:^|\s)--max-time(?:=|\s+)([0-9]+(?:\.[0-9]+)?)`).FindStringSubmatch(command)
	if len(maxTimeMatch) != 2 {
		return errors.New("aura healthcheck curl --max-time is missing")
	}
	maxTimeSeconds, err := strconv.ParseFloat(maxTimeMatch[1], 64)
	if err != nil || time.Duration(maxTimeSeconds*float64(time.Second)) <= serverBudget {
		return fmt.Errorf("curl max-time %q must exceed server budget %s", maxTimeMatch[1], serverBudget)
	}
	for name, value := range map[string]string{
		"interval":     hc.Interval,
		"timeout":      hc.Timeout,
		"start_period": hc.StartPeriod,
	} {
		duration, err := time.ParseDuration(value)
		if err != nil || duration <= 0 {
			return fmt.Errorf("aura healthcheck %s must be an explicit positive duration", name)
		}
	}
	composeTimeout, _ := time.ParseDuration(hc.Timeout)
	if composeTimeout <= time.Duration(maxTimeSeconds*float64(time.Second)) {
		return fmt.Errorf("compose timeout %s must exceed curl max-time %ss", composeTimeout, maxTimeMatch[1])
	}
	if hc.Retries <= 0 {
		return errors.New("aura healthcheck retries must be explicit and positive")
	}
	return nil
}

func assertReadyzStatus(t *testing.T, baseURL string, want int) {
	t.Helper()
	resp, err := http.Get(baseURL + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != want {
		t.Fatalf("GET /readyz status = %d, want %d", resp.StatusCode, want)
	}
}

type schedulerLifecycleFunc func(context.Context) error

func (f schedulerLifecycleFunc) Start(ctx context.Context) error { return f(ctx) }

type failingServeListener struct {
	once   sync.Once
	closed chan struct{}
}

func newFailingServeListener() *failingServeListener {
	return &failingServeListener{closed: make(chan struct{})}
}

func (l *failingServeListener) Accept() (net.Conn, error) {
	return nil, errors.New("listener accept sentinel")
}

func (l *failingServeListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func (*failingServeListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)}
}

func TestServeListenerBindFailureIsSynchronous(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("occupy listener: %v", err)
	}
	t.Cleanup(func() { _ = occupied.Close() })

	listener, err := bindServeListener(occupied.Addr().String(), net.Listen)
	if err == nil {
		if listener != nil {
			_ = listener.Close()
		}
		t.Fatal("address-in-use bind returned nil error")
	}
	if listener != nil {
		t.Fatal("bind failure returned a live listener")
	}
}

func TestServeListenerRuntimeFailureCancelsScheduler(t *testing.T) {
	snapshot := readiness.NewSnapshot(readiness.Config{MigrationCompatible: true, SchedulerEnabled: true})
	snapshot.MarkListenerBound()
	listener := newFailingServeListener()
	srv := &http.Server{Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})}
	schedulerCancelled := make(chan struct{})
	scheduler := schedulerLifecycleFunc(func(ctx context.Context) error {
		<-ctx.Done()
		close(schedulerCancelled)
		return nil
	})

	err := runServeComponents(context.Background(), snapshot, listener, srv, scheduler, func() {
		_ = srv.Shutdown(context.Background())
	})
	if err == nil || !strings.Contains(err.Error(), "listener accept sentinel") {
		t.Fatalf("runtime listener error = %v, want propagated sentinel", err)
	}
	select {
	case <-schedulerCancelled:
	case <-time.After(time.Second):
		t.Fatal("runtime listener failure did not cancel/join scheduler")
	}
	if !containsReadinessCode(snapshot.Reasons(), readiness.CodeListenerUnavailable) {
		t.Fatalf("runtime listener failure reasons = %v, want listener_unavailable", snapshot.Reasons())
	}
}

func TestServeListenerErrServerClosedIsCleanDuringDrain(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	snapshot := readiness.NewSnapshot(readiness.Config{MigrationCompatible: true})
	snapshot.MarkListenerBound()
	srv := &http.Server{Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	scheduler := schedulerLifecycleFunc(func(ctx context.Context) error {
		<-ctx.Done()
		return nil
	})

	err = runServeComponents(ctx, snapshot, listener, srv, scheduler, func() {
		_ = srv.Shutdown(context.Background())
	})
	if err != nil {
		t.Fatalf("intentional shutdown returned %v, want nil", err)
	}
}

func containsReadinessCode(codes []readiness.Code, want readiness.Code) bool {
	return slices.Contains(codes, want)
}
