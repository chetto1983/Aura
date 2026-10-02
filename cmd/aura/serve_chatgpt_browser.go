package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"regexp"
	"sync"
	"time"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/chatgptplan"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
)

//go:embed chatgpt-browser.mjs
var chatGPTBrowserScript []byte

var chatGPTBrowserSessionPattern = regexp.MustCompile(`^chatgpt-[a-f0-9]{24}$`)

type sandboxChatGPTBrowser struct{ router *usersandbox.SandboxRouter }

func (b sandboxChatGPTBrowser) Open(ctx context.Context, session string) (chatgptplan.BrowserSession, error) {
	if !chatGPTBrowserSessionPattern.MatchString(session) {
		return nil, errors.New("invalid ChatGPT browser session")
	}
	h, err := b.router.Route(ctx)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(chatGPTBrowserScript)
	path := "/tmp/.aura-chatgpt-browser-" + hex.EncodeToString(digest[:8]) + ".mjs"
	if err = b.router.WriteFile(ctx, h, path, chatGPTBrowserScript); err != nil {
		return nil, err
	}
	in, commands := io.Pipe()
	out, output := io.Pipe()
	handle, err := b.router.ExecStream(ctx, h, usersandbox.ExecRequest{
		Command: "node " + path + " " + session, Dir: "/workspace",
	}, in, output)
	if err != nil {
		_ = in.Close()
		_ = commands.Close()
		_ = out.Close()
		_ = output.Close()
		return nil, err
	}
	s := newChatGPTBrowserSession(handle, commands, out, output)
	ready, err := s.next(ctx, "listening")
	if err == nil {
		parsed, parseErr := url.Parse(ready.RedirectURI)
		if parseErr != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" || parsed.Port() == "" || parsed.Path != "/auth/callback" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil {
			err = errors.New("invalid ChatGPT browser listener")
		} else {
			s.redirectURI = ready.RedirectURI
		}
	}
	if err != nil {
		_ = s.Close()
		return nil, err
	}
	return s, nil
}

type chatGPTBrowserEvent struct {
	Type        string `json:"type"`
	RedirectURI string `json:"redirect_uri"`
	Query       string `json:"query"`
}

type chatGPTBrowserSession struct {
	handle      agui.BrowserRelayHandle
	commands    io.WriteCloser
	output      *io.PipeReader
	events      chan chatGPTBrowserEvent
	callbacks   chan string
	ended       chan struct{}
	readDone    chan struct{}
	closing     chan struct{}
	once        sync.Once
	redirectURI string
	closeErr    error
	waitErr     error
	cleanupErr  bool
}

func newChatGPTBrowserSession(handle agui.BrowserRelayHandle, commands io.WriteCloser, out *io.PipeReader, output *io.PipeWriter) *chatGPTBrowserSession {
	s := &chatGPTBrowserSession{handle: handle, commands: commands, output: out,
		events: make(chan chatGPTBrowserEvent, 4), callbacks: make(chan string, 1),
		ended: make(chan struct{}), readDone: make(chan struct{}), closing: make(chan struct{})}
	go func() {
		code, err := handle.Wait()
		if err != nil || code != 0 {
			s.waitErr = errors.New("ChatGPT browser process failed")
		}
		_ = output.Close()
		close(s.ended)
	}()
	go s.read()
	return s
}

func (s *chatGPTBrowserSession) read() {
	defer close(s.readDone)
	defer close(s.events)
	defer close(s.callbacks)
	scanner := bufio.NewScanner(s.output)
	scanner.Buffer(make([]byte, 4096), 32<<10)
	for scanner.Scan() {
		var event chatGPTBrowserEvent
		if json.Unmarshal(scanner.Bytes(), &event) != nil {
			continue
		}
		if event.Type == "cleanup_error" {
			s.cleanupErr = true
		}
		if event.Type == "callback" {
			select {
			case s.callbacks <- event.Query:
			case <-s.closing:
			}
		} else {
			select {
			case s.events <- event:
			case <-s.closing:
			}
		}
	}
	_, _ = io.Copy(io.Discard, s.output)
}

func (s *chatGPTBrowserSession) next(ctx context.Context, kind string) (chatGPTBrowserEvent, error) {
	select {
	case event, ok := <-s.events:
		if ok && event.Type == kind {
			return event, nil
		}
	case <-ctx.Done():
		return chatGPTBrowserEvent{}, ctx.Err()
	}
	return chatGPTBrowserEvent{}, errors.New("ChatGPT browser unavailable; retry sign-in")
}

func (s *chatGPTBrowserSession) RedirectURI() string { return s.redirectURI }

func (s *chatGPTBrowserSession) Navigate(ctx context.Context, authURL string) error {
	data, err := json.Marshal(map[string]string{"type": "navigate", "url": authURL})
	if err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { _ = s.commands.Close() })
	defer stop()
	if _, err = s.commands.Write(append(data, '\n')); err != nil {
		return err
	}
	_, err = s.next(ctx, "navigated")
	return err
}

func (s *chatGPTBrowserSession) Callback(ctx context.Context) (url.Values, error) {
	select {
	case raw, ok := <-s.callbacks:
		if ok {
			return url.ParseQuery(raw)
		}
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return nil, errors.New("ChatGPT browser closed; retry sign-in")
}

func (s *chatGPTBrowserSession) Close() error {
	s.once.Do(func() {
		close(s.closing)
		_ = s.commands.Close()
		timer := time.NewTimer(6 * time.Second)
		defer timer.Stop()
		select {
		case <-s.ended:
		case <-timer.C:
			s.handle.Kill()
			<-s.ended
			s.closeErr = errors.New("ChatGPT browser required forced cleanup")
		}
		_ = s.output.Close()
		<-s.readDone
		if s.closeErr == nil && (s.waitErr != nil || s.cleanupErr) {
			s.closeErr = errors.New("ChatGPT browser cleanup failed")
		}
	})
	return s.closeErr
}
