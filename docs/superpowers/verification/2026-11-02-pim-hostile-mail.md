# PIM conversion against hostile mail: 2026-11-02

Scope: `aura-pim-mcp` `main` at `204a7fd`, the conversion a mail body passes through before a model or the watch reads it. Run on a local image at the deployment's 512 MiB and half a CPU, over IMAP on GreenMail and Dovecot. This is not the lab VM run: the spec's G3 stays owed, and the mail watch's first run on a real mailbox is still to come.

Four independent reviews read the fixes, the last one at `89a1c9b`, and each hostile shape below was reproduced before it was fixed. The last 8 commits, `89a1c9b..204a7fd`, had no independent review.

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
| `<img width="` + 100,000 spaces + `!">` | 100 KB | A cubic number regex, from a fix of the same night, pinned the conversion places | Read in linear time |
| Selector cost that came out NaN | under 1 KB | Passed the check that exists to refuse it, so the selector hang came back | Refused on both paths |
| `:focus-within` in a costly selector | 60 KB | Priced at 1 while it walks a subtree | Priced by elements, and an unknown bare name no longer reads as cheap |
| One start tag of about 366,000 attributes | 1 MiB | 33 s of a full core, quadratic, with no budget read | Refused in 126 to 181 ms |

The abort that started this was a test: `<b>` at the depth limit needs 307 KB in a fresh process and overflowed a 256 KB test thread, so it passed or failed by JIT tier. The crash it led to was not, and neither the depth limit nor a dedicated thread could hold it, because 1 MiB of pasted text would need about 386 MB of stack.

## Numbers

- Conversion runs on its own thread with an explicit 8 MiB stack, read back from `pthread_getattr_np` at `ulimit -s` 8192, unlimited and 1024.
- Worst mail allowed: 608 KB of stack on the real path, first mail after a start. Margin 13 times.
- A thread that does not return is abandoned, at most two at once, and past that conversions refuse. Tests cover this with a hung delegate.
- Suite at the final tree: 1,746 passed, 0 failed, twice in a row, and the whole suite once more in the forced-cold mode (`DOTNET_ReadyToRun=0`, `TieredPGO=1`, `InstrumentOnlyHotCode=0`, `TC_CallCounting=0`, `TC_QuickJitForLoops=1`).
- One long-lived server at `b4c5aae` through 40 hostile calls, on `/admin/changes` and `get_email_details`: up after each, conversion places free again, no hidden text leaked, 238 MiB of 512, no restart.
- At `89a1c9b`, not repeated at the final tree: 20 fresh servers, one mail each, forced-cold and `ulimit -s unlimited`, none died. Cold fuzz of about 29,700 shapes at 200 KB, 0 crashes, 69 slow, of which 21 lines were read, all at or under 2,003 ms against a 2 s budget; the rest were lost with the container.
- A newsletter of 62,890 characters, at `8be80a2`: Markdown 63 ms cold and 35 warm, plain text 66 and 41, warm-up 302 ms once per process.

## What this does not prove

- That the worst shape was found. The var() bound holds by construction, the rest is the worst that could be built.
- Real Gmail, Microsoft 365 or Outlook.com. MSAL and Google's token endpoint were not run. Kubernetes was not run. Hidden-text rules were measured in Chromium 131 only, and real clients change CSS.
- A thread that truly never returns on the real image. The JIT hang seen near a stack boundary was not reproduced.
- Memory with several conversions at their worst at once. One under 512 MiB became a caught failure at a 465 MB peak; three at once were not measured.

## Open, and accepted for the first run

- One 268 KB mail just under a bound holds both conversion places for 48 to 57 s at half a CPU. Other mail then gets an empty snippet and an `omitted` body, and a mail converted in that window keeps its empty snippet, because the cursor moves past it. The watch does not stop. A stranger sending one a minute could keep it that way. The bound stays at 2×10⁹, because lowering it refuses large ordinary mail. Look at this before G3.
- A bare table over about 320 KB is refused, a known false positive. Visible text still removed in four recorded shapes, listed in the plan.
- A false tag in a script or comment that opens a quote may hide a real long tag from the attribute counter. Noticed, the mail was not built.
