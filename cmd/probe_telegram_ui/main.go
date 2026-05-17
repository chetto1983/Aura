// probe_telegram_ui drives a real Telegram Web client via Chrome DevTools
// Protocol to verify that Aura's tool-call status pane is actually visible
// to the user. Unit + httptest E2E covers the wire format; this probe
// covers the rendering you see with your eyes.
//
// # Usage
//
//	# 1. Launch Chrome with remote debugging (one-time setup):
//	#    Windows: Close all Chrome windows first, then:
//	#      "C:\Program Files\Google\Chrome\Application\chrome.exe" ^
//	#        --remote-debugging-port=9222 ^
//	#        --user-data-dir="%USERPROFILE%\.chrome-cdp-profile"
//	#    Log into https://web.telegram.org/a/ inside this Chrome.
//	#
//	# 2. Open the chat with your Aura bot in that Chrome.
//	#
//	# 3. Run the probe:
//	#      go run ./cmd/probe_telegram_ui \
//	#        -prompt "trova in memoria phantom guard" \
//	#        -timeout 60s
//
// The probe attaches to the running Chrome via ws://127.0.0.1:9222, finds
// the Telegram tab, sends the prompt into the composer, and polls the
// latest incoming message bubble. It records the unique sequence of bodies
// the bubble passed through (Aura's progressive edits) and asserts:
//
//   - the "🛠 Sto lavorando…" status header appeared at least once
//   - a known tool name (search_memory / web_search / execute_shell)
//     appeared at least once
//   - the final body is a clean answer (no 🛠 / no 🧠 chrome)
//
// Why attach instead of launching: the user is already logged into
// Telegram. A fresh Chrome would need a new login + MFA. The attach
// approach piggybacks on an existing session.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

type flags struct {
	cdpURL     string
	prompt     string
	timeout    time.Duration
	tabURLLike string
	pollEvery  time.Duration
	verbose    bool
	expectTool string
}

func main() {
	f := parseFlags()

	rootCtx, cancel := context.WithTimeout(context.Background(), f.timeout)
	defer cancel()

	// 1) Discover the existing Chrome via CDP. chromedp.NewRemoteAllocator
	// rewrites http://host:port into the ws:// scheme via the /json/version
	// endpoint — keep the rewrite ON (omit NoModifyURL).
	allocCtx, cancelAlloc := chromedp.NewRemoteAllocator(rootCtx, f.cdpURL)
	defer cancelAlloc()

	// 2) Open a browser context. NoFirstRun avoids opening an extra tab.
	ctx, _ := chromedp.NewContext(allocCtx, chromedp.WithLogf(log.Printf))
	// IMPORTANT: do not defer cancelCtx — cancel would close the active tab
	// we attach to, leaving Chrome with zero tabs and shutting the browser
	// down. The OS reclaims everything on process exit anyway.
	if err := chromedp.Run(ctx); err != nil {
		exit("init cdp context: %v", err)
	}

	// Find the Telegram tab.
	tabID, tabURL, err := findTelegramTab(ctx, f.tabURLLike)
	if err != nil {
		exit("find telegram tab: %v", err)
	}
	fmt.Fprintf(os.Stderr, "attaching to telegram tab: id=%s url=%s\n", tabID, tabURL)

	// Attach to the existing tab. Again — no defer cancel; closing the
	// child context closes the tab.
	tabCtx, _ := chromedp.NewContext(ctx, chromedp.WithTargetID(target.ID(tabID)))

	// 3) Sanity-check by waiting for the composer specifically (it's the
	// thing we actually need). #main-content doesn't exist in the "A"
	// client; .messages-container exists even on the chat-list view, so
	// neither is a reliable readiness signal. The composer is.
	if err := chromedp.Run(tabCtx,
		chromedp.WaitVisible(`#editable-message-text`, chromedp.ByQuery),
	); err != nil {
		exit("wait for composer (open a chat with the bot first): %v", err)
	}
	fmt.Fprintf(os.Stderr, "telegram chat UI ready (composer visible)\n")

	// 4) Send the prompt.
	if err := sendPrompt(tabCtx, f.prompt); err != nil {
		exit("send prompt: %v", err)
	}
	fmt.Fprintf(os.Stderr, "prompt sent: %q\n", f.prompt)

	// 5) Poll the latest incoming message bubble; record unique bodies.
	bodies, err := pollIncomingBubble(tabCtx, f.pollEvery, f.timeout, f.verbose)
	if err != nil {
		exit("poll incoming bubble: %v", err)
	}

	fmt.Fprintf(os.Stderr, "\n=== captured %d unique bodies ===\n", len(bodies))
	for i, b := range bodies {
		fmt.Fprintf(os.Stderr, "[%d] %s\n", i, oneline(b, 200))
	}

	// 6) Assertions.
	//
	// IMPORTANT: Telegram renders emojis as <img> elements, so .innerText
	// strips them entirely (🛠 and 🧠 don't appear in the captured text).
	// Assert on the Italian copy text instead, which is what the user
	// actually reads.
	pass := true
	if !anyContains(bodies, "Sto lavorando") {
		fmt.Fprintf(os.Stderr, "\nFAIL: status header 'Sto lavorando…' never appeared (pane never rendered)\n")
		pass = false
	}
	if f.expectTool != "" && !anyContains(bodies, f.expectTool) {
		fmt.Fprintf(os.Stderr, "\nFAIL: expected tool name %q never appeared in any body\n", f.expectTool)
		pass = false
	}
	if len(bodies) > 0 {
		last := bodies[len(bodies)-1]
		// "chrome" = the pane/CoT text. Once Aura streams the final answer,
		// none of these should remain.
		chromeMarkers := []string{"Sto lavorando", "strumento usato", "strumenti usati", "round 1 (", "in corso"}
		for _, m := range chromeMarkers {
			if strings.Contains(last, m) {
				fmt.Fprintf(os.Stderr, "\nFAIL: final body still carries chrome marker %q: %s\n",
					m, oneline(last, 240))
				pass = false
				break
			}
		}
	} else {
		fmt.Fprintf(os.Stderr, "\nFAIL: no bodies captured — message never appeared?\n")
		pass = false
	}

	if !pass {
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "\nPASS\n")
}

func parseFlags() flags {
	var f flags
	flag.StringVar(&f.cdpURL, "cdp", "http://127.0.0.1:9222", "Chrome DevTools URL (http or ws)")
	flag.StringVar(&f.prompt, "prompt", "test", "prompt to send to the bot")
	flag.DurationVar(&f.timeout, "timeout", 90*time.Second, "total probe timeout")
	flag.StringVar(&f.tabURLLike, "tab-url", "web.telegram.org", "substring identifying the Telegram tab")
	flag.DurationVar(&f.pollEvery, "poll", 300*time.Millisecond, "poll interval for the bubble body")
	flag.BoolVar(&f.verbose, "verbose", false, "print every body change as it happens")
	flag.StringVar(&f.expectTool, "expect-tool", "", "if set, assert this tool name appears in some body")
	flag.Parse()
	return f
}

func exit(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "probe_telegram_ui: "+format+"\n", args...)
	os.Exit(2)
}

// --- helpers ---

// findTelegramTab queries the CDP /json endpoint for an open tab whose URL
// contains `urlLike`, returning its target ID.
func findTelegramTab(ctx context.Context, urlLike string) (string, string, error) {
	infos, err := chromedp.Targets(ctx)
	if err != nil {
		return "", "", fmt.Errorf("list targets: %w", err)
	}
	for _, t := range infos {
		if t.Type != "page" {
			continue
		}
		if strings.Contains(t.URL, urlLike) {
			return string(t.TargetID), t.URL, nil
		}
	}
	return "", "", fmt.Errorf("no tab with url containing %q (open https://web.telegram.org/a/ in your Chrome first)", urlLike)
}

// sendPrompt focuses the composer input and types the prompt, then sends.
// Selectors target web.telegram.org/a/ (the "A" client). If they break
// after a Telegram redesign, update here.
func sendPrompt(ctx context.Context, prompt string) error {
	// The "A" client composer is a contenteditable div with id=editable-message-text.
	// Fall back to a div[contenteditable=true] inside the composer footer.
	const composerSel = `#editable-message-text, div.input-message-input[contenteditable="true"]`
	const sendBtnSel = `button.send, button[aria-label*="Send" i], button.Button.send`

	if err := chromedp.Run(ctx,
		chromedp.WaitVisible(composerSel, chromedp.ByQuery),
		chromedp.Click(composerSel, chromedp.ByQuery),
		chromedp.SendKeys(composerSel, prompt, chromedp.ByQuery),
	); err != nil {
		return fmt.Errorf("type into composer: %w", err)
	}

	// Prefer the Send button click; fallback to Enter key.
	clickErr := chromedp.Run(ctx, chromedp.Click(sendBtnSel, chromedp.ByQuery))
	if clickErr != nil {
		// Send via Enter — KeyEvent on the focused composer.
		if err := chromedp.Run(ctx, chromedp.KeyEvent("\r")); err != nil {
			return fmt.Errorf("press Enter on composer: %w", err)
		}
	}
	return nil
}

// pollIncomingBubble repeatedly reads the text content of the last incoming
// message bubble in the chat. Returns the deduplicated sequence of bodies
// observed (Aura's progressive edits show as DOM updates).
//
// Stops when EITHER the body is stable for ~3s AND its content doesn't
// contain the status chrome anymore (= final answer landed), OR the global
// timeout fires.
func pollIncomingBubble(ctx context.Context, every, total time.Duration, verbose bool) ([]string, error) {
	// web.telegram.org/a/ ("A" client) — verified 2026-05-17:
	//   #MiddleColumn .MessageList .Message  (30 entries in the live chat)
	//   .Message .text-content                (inner text container)
	// We grab the LAST :not(.own) message in the list — Aura's reply lands
	// there as a fresh node and is progressively edited in place. If the
	// .own class name changes after a Telegram redesign, update here.
	const bubbleScript = `(() => {
		const msgs = document.querySelectorAll('#MiddleColumn .MessageList .Message:not(.own)');
		if (msgs.length === 0) return '';
		const last = msgs[msgs.length - 1];
		const text = last.querySelector('.text-content');
		return (text ? text.innerText : last.innerText) || '';
	})()`

	deadline := time.Now().Add(total)
	ticker := time.NewTicker(every)
	defer ticker.Stop()

	var bodies []string
	var last string
	var stableSince time.Time
	const stableFor = 3 * time.Second

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return bodies, ctx.Err()
		case <-ticker.C:
		}

		var body string
		err := chromedp.Run(ctx, chromedp.Evaluate(bubbleScript, &body))
		if err != nil {
			// Bubble not present yet; keep waiting.
			continue
		}
		body = strings.TrimSpace(body)
		if body == "" {
			continue
		}
		if body != last {
			if verbose {
				fmt.Fprintf(os.Stderr, "  edit: %s\n", oneline(body, 160))
			}
			bodies = append(bodies, body)
			last = body
			stableSince = time.Time{}
			continue
		}
		// Body unchanged.
		if stableSince.IsZero() {
			stableSince = time.Now()
		}
		// Terminate early once we've seen a stable, chrome-free body.
		// Same emoji-stripping concern as the assertion block — use text
		// markers rather than the wrench/brain emojis.
		if time.Since(stableSince) >= stableFor {
			hasChrome := strings.Contains(body, "Sto lavorando") ||
				strings.Contains(body, "in corso") ||
				strings.Contains(body, "strumento usato") ||
				strings.Contains(body, "strumenti usati")
			if !hasChrome {
				return bodies, nil
			}
		}
	}
	if len(bodies) == 0 {
		return nil, errors.New("timed out waiting for any incoming message body")
	}
	return bodies, nil
}

func anyContains(bodies []string, sub string) bool {
	for _, b := range bodies {
		if strings.Contains(b, sub) {
			return true
		}
	}
	return false
}

func oneline(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " | ")
	if len([]rune(s)) > maxLen {
		r := []rune(s)
		return string(r[:maxLen-1]) + "…"
	}
	return s
}
