"""The section a passage belongs to, taken from the document's own table of contents.

`heading_path` has been on Passage, in the ArcadeDB schema and in the retrieval response
since the beginning, and it was empty on all 445 passages of the ArcadeDB manual: the
chunker cuts by size and has no idea where a section starts. That is the field a question
like "the options of vector.fuse" is really asking about -- the answer is §6.4.19
Group-By Retrieval, a named section, not a 4,000-character window that happens to contain
the words.

Measured 2026-09-09 against two very different PDFs, and this is why the outline is the
source rather than a pattern: the ArcadeDB manual numbers its sections (`6.4.19.`) while
the Italian Constitution uses `Art. N`, `Titolo <roman>` and `Parte <roman>`. A regex
tuned to the first finds 2 candidates in the second. Their outlines describe both -- 1,290
entries and 37 -- and every one anchored into the extracted text in document order,
1,290/1,290 and 37/37, with the manual's anchors spread from character 1 to 1,944,867
rather than piling up in its contents listing.

Anchoring into the text Tika already produced is the point: no second extractor, no page
arithmetic, and every citation offset already issued stays valid.
"""
import dataclasses
import pathlib
import re

_PDF_SUFFIX = ".pdf"
_MARKDOWN_SUFFIXES = (".md", ".markdown")

# A contents entry renders as the title, a run of dot leaders and a page number:
# "1.1. ArcadeDB Documentation . . . . . . . . . 2". A body heading has none of that.
# Without this the forward scan can anchor an entire outline inside the listing, because a
# listing repeats every title in the order the outline gives them.
_CONTENTS_TAIL = re.compile(r"^[ \t]*(?:\.[ \t]*){3,}\d*[ \t]*$")


# An outline whose anchors pile up at the end of the document did not land in the document:
# it matched a trailing index. The median says this and the first anchor does not -- the
# Italian Constitution's first entry is "INDICE" and it correctly matches at character 160,
# while everything after it sits in a 538-character index at the very end.
#
# Measured 2026-09-09: the ArcadeDB manual's median anchor is at 1,092,763 of 1,944,898
# (0.56, a document being walked front to back) and the Constitution's at 96,678 of 96,987
# (0.997), because its outline titles do not match the typography its body uses and their
# only exact occurrences are in that index. 0.8 separates 0.56 from 0.997 with room on both
# sides and claims nothing beyond those two measurements. Above it the outline is refused
# entirely and heading_path stays empty -- which every reader already handles -- rather than
# being confidently wrong on every passage.
_MAX_MEDIAN_ANCHOR = 0.8


@dataclasses.dataclass(frozen=True, slots=True)
class Anchor:
    """Where a section starts in the extracted text, and the titles above it."""

    offset: int
    heading_path: tuple[str, ...]


def titles_of(path: str) -> list[tuple[int, str]]:
    """The outline as (depth, title), or [] for anything without a readable one.

    Never raises: a document with no outline, a damaged one, or a format neither branch
    below reads must leave the document indexed exactly as it is today, not fail its
    ingest.
    """
    suffix = pathlib.PurePath(path).suffix.lower()
    if suffix in _MARKDOWN_SUFFIXES:
        return _markdown_titles(path)
    if suffix != _PDF_SUFFIX:
        return []
    try:
        from pypdf import PdfReader

        outline = PdfReader(path).outline
    except Exception:  # noqa: BLE001 - no outline is a normal document, never an error
        return []
    return _walk(outline)


def _markdown_titles(path: str) -> list[tuple[int, str]]:
    """The headings of a Markdown file, as (depth, title), read by a CommonMark parser.

    Markdown keeps its outline in the body rather than in a structure beside it, so the PDF
    branch finds nothing at all here. Measured 2026-09-09 through the documents MCP server:
    prd.md and aura-quality-snapshot.md returned passages whose locator carried a character
    span and no heading_path, while the passage text plainly contained the line
    "## 16. Observability and operator experience" -- the heading sat in the evidence and
    was missing from the field meant to name it.

    A parser and not a pattern, for reasons a pattern loses one at a time: a line opening
    with '#' inside a fenced block is a shell comment, `## Title ##` closes with hashes that
    are not part of the title, and a setext heading is underlined rather than prefixed.
    Measured on this repo 2026-09-09 against a hand-written ATX regex: both find prd.md's
    20 headings, both skip the fence, but the regex misses setext entirely and has to track
    fence state itself. markdown-it-py is the CommonMark reference implementation for
    Python, pure Python and dependency-light.

    The title comes back as the parser's inline content, which is what the line still reads
    in the EXTRACTED text anchors_in searches -- verified 20/20 on prd.md.
    """
    try:
        text = pathlib.Path(path).read_text(encoding="utf-8", errors="replace")
        from markdown_it import MarkdownIt

        tokens = MarkdownIt("commonmark").parse(text)
    except (OSError, ImportError):  # unreadable or unparseable: no headings, never a failed ingest
        return []
    found: list[tuple[int, str]] = []
    for token, following in zip(tokens, tokens[1:]):
        if token.type != "heading_open" or following.type != "inline":
            continue
        title = following.content.strip()
        if title:
            found.append((int(token.tag[1:]), title))
    return found


def _walk(items: object, depth: int = 0) -> list[tuple[int, str]]:
    found: list[tuple[int, str]] = []
    for item in items or ():  # type: ignore[union-attr]
        if isinstance(item, list):
            found += _walk(item, depth + 1)
            continue
        title = str(getattr(item, "title", "") or "").strip()
        if title:
            found.append((depth, title))
    return found


def _has_boundaries(text: str, at: int, length: int) -> bool:
    """Whether the match stands on its own rather than inside a longer word or title.

    Measured on the Italian Constitution, whose outline offers the bare string "Titolo I":
    it occurs 16 times in the text because it also opens "Titolo II" and "Titolo III", and
    matching one of those walks the cursor into the wrong section, dragging every later
    anchor along with it.
    """
    before = text[at - 1] if at else "\n"
    after = text[at + length] if at + length < len(text) else "\n"
    return not (before.isalnum() or after.isalnum())


def _find_heading(text: str, title: str, cursor: int) -> int:
    """The next occurrence of title that is not a contents-listing entry.

    Falls back to the first occurrence when every one looks like a listing: a section whose
    only mention is in the document's own contents is better anchored imprecisely than
    dropped, and the caller's forward cursor keeps the order sane.
    """
    first = -1
    at = text.find(title, cursor)
    while at != -1:
        if first == -1:
            first = at
        line_end = text.find("\n", at)
        tail = text[at + len(title):line_end if line_end != -1 else len(text)]
        if _has_boundaries(text, at, len(title)) and not _CONTENTS_TAIL.match(tail):
            return at
        at = text.find(title, at + len(title))
    return first


def anchors_in(text: str, entries: list[tuple[int, str]]) -> list[Anchor]:
    """Locate each outline title in the text, in order, and carry its ancestry.

    The scan is forward-only, which keeps the anchors in document order. A title the text
    does not carry is skipped, because a heading placed by guesswork is worse than a
    passage with no heading at all.
    """
    anchors: list[Anchor] = []
    stack: list[tuple[int, str]] = []
    cursor = 0
    for depth, title in entries:
        offset = _find_heading(text, title, cursor)
        if offset == -1:
            continue
        while stack and stack[-1][0] >= depth:
            stack.pop()
        stack.append((depth, title))
        anchors.append(Anchor(offset=offset, heading_path=tuple(t for _, t in stack)))
        cursor = offset + len(title)
    return anchors if _reaches_the_body(anchors, len(text)) else []


def _reaches_the_body(anchors: list[Anchor], length: int) -> bool:
    if not anchors or length <= 0:
        return False
    median = sorted(a.offset for a in anchors)[len(anchors) // 2]
    return median / length <= _MAX_MEDIAN_ANCHOR
