# PIM conversion against hostile mail: 2026-11-02

Scope: `aura-pim-mcp` branch `aura-watch`, HEAD `89a1c9b`, the conversion a mail body passes through before a model or the watch reads it. Run on a local image at the deployment's 512 MiB and half a CPU, over IMAP on GreenMail and Dovecot. This is not the lab VM run: the spec's G3 stays owed, and the mail watch's first run on a real mailbox is still to come.

Three independent reviews read the fixes, and each hostile shape below was reproduced before it was fixed.

## What was found

| Shape | Size | At the older build | Now |
|---|---:|---|---|
| CSS variables that paste each other, 17 levels | 560 B | Exit 139, `Stack overflow.`, the server down on every listing of that mail | `omitted`, server up |
| A long sum pasted 128 times | 4.8 KB | Exit 139, same | `omitted`, server up |
| Selector with 20 `*` over 150 nested `div` | under 1 KB | Still running at 6 s on both paths; review estimate 10²⁴ paths, no budget read | Refused in 143 to 244 ms |
| 100,000 `<x>` then 185,000 `</y>` | 1 MiB | Over 90 s inside the parser, no budget read | Refused from the raw text in 115 to 183 ms |
| `</>` and `--!` floods | | Overflow 8 MiB on cold code, process killed | Refused, bound 1,024 against a measured failure at about 43,500 |
| `<p><b a{i}>x</p>` misnested | 33 KB | 1.2 GB allocated with no budget | Capped |
| MIME nested past 32 levels | | Read as an empty body, so a mail could look empty to a model | Says the body is left out |

The abort that started this was a test: `<b>` at the depth limit needs 307 KB in a fresh process and overflowed a 256 KB test thread, so it passed or failed by JIT tier. The crash it led to was not, and neither the depth limit nor a dedicated thread could hold it, because 1 MiB of pasted text would need about 386 MB of stack.

## Numbers

- Conversion runs on its own thread with an explicit 8 MiB stack, read back from `pthread_getattr_np` at `ulimit -s` 8192, unlimited and 1024.
- Worst mail allowed: 608 KB of stack on the real path, first mail after a start. Margin 13 times.
- A thread that does not return is abandoned, at most two at once, and past that conversions refuse. Tests cover this with a hung delegate.
- Suite: 1,712 passed, 0 failed, three runs in a row. Deep and hostile tests in the forced-cold mode (`DOTNET_ReadyToRun=0`, `TieredPGO=1`, `InstrumentOnlyHotCode=0`, `TC_CallCounting=0`, `TC_QuickJitForLoops=1`): 443 passed.
- One long-lived server through 29 hostile mails, on `/admin/changes` and `get_email_details`: up after each, the next ordinary mail converted after each, no hidden text leaked, slowest details call 1.24 s, 172 MiB of 512.
- 20 fresh servers, one mail each, forced-cold and `ulimit -s unlimited`: none died.
- Cold fuzz, about 29,700 shapes at 200 KB: 0 crashes, 69 slow. 21 of the 69 lines were read, all at or under 2,003 ms against a 2 s budget; the rest were lost with the container.
- A newsletter of 62,890 characters: Markdown 63 ms cold and 35 warm, plain text 66 and 41, warm-up 302 ms once per process.

## What this does not prove

- That the worst shape was found. The var() bound holds by construction, the rest is the worst that could be built.
- Real Gmail, Microsoft 365 or Outlook.com. MSAL and Google's token endpoint were not run. Kubernetes was not run. Hidden-text rules were measured in Chromium 131 only, and real clients change CSS.
- A thread that truly never returns on the real image. The JIT hang seen near a stack boundary was not reproduced.
- Memory with several conversions at their worst at once. One under 512 MiB became a caught failure at a 465 MB peak; three at once were not measured.

## Open, and accepted for the first run

- One 268 KB mail just under a bound holds both conversion places for 48 to 57 s at half a CPU. Other mail then gets an empty snippet and an `omitted` body, and a mail converted in that window keeps its empty snippet, because the cursor moves past it. The watch does not stop. A stranger sending one a minute could keep it that way. The bound stays at 2×10⁹, because lowering it refuses large ordinary mail. Look at this before G3.
- A bare table over about 320 KB is refused, a known false positive. Visible text still removed in four recorded shapes, listed in the plan.
- `WarmedUp` is a `Lazy` that caches an exception and `WarmUp` sits outside the `try` in `SnippetPage.Of` and `MailSnippet.Of`, so a failed warm-up would fail every page. It has not happened.
