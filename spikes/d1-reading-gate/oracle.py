"""The deterministic sufficiency oracle: does a text carry every content token of the gold answer?

Lenient on purpose and stated as such in FINDINGS: token containment, not meaning. A number is
compared without thousand separators and without a trailing ".0", so "11.956" and "11956.0" both
match "11956"; a percentage keeps its digits ("0.3%" -> "0.3"). It cannot tell "36 mesi" from
"36 giorni", and a one-digit token ("5") is almost always present somewhere.
"""

import re
import unicodedata

_STOP = {"il", "lo", "la", "i", "gli", "le", "di", "del", "della", "per", "una", "un", "e", "a", "in",
         "che", "totale", "non"}
_TOKEN = re.compile(r"[a-z]+|\d+(?:[.,]\d+)*")


def _fold(text: str) -> str:
    text = unicodedata.normalize("NFKD", text.lower())
    return "".join(c for c in text if not unicodedata.combining(c))


def _number(tok: str) -> str:
    if re.fullmatch(r"\d{1,3}(?:\.\d{3})+", tok):
        tok = tok.replace(".", "")
    tok = tok.replace(",", ".")
    return tok[:-2] if tok.endswith(".0") else tok


def tokens(text: str) -> set[str]:
    return {_number(t) if t[0].isdigit() else t for t in _TOKEN.findall(_fold(text))}


def answer_tokens(answer: str) -> set[str]:
    return {t for t in tokens(answer) if t not in _STOP}


def weak(answer: str) -> bool:
    """An answer the oracle cannot judge: a token so short that every passage holds it."""
    return any(len(t) < 2 or (t.isalpha() and len(t) < 3) for t in answer_tokens(answer))


_IDENT = re.compile(r"\b(?=[a-z0-9-]*\d)(?=[a-z0-9-]*[a-z])[a-z0-9]+(?:-[a-z0-9]+)*\b")


def identifiers(query: str) -> set[str]:
    """The letter+digit codes a question names (A006, P067, RIC-10959, M10), folded."""
    return set(_IDENT.findall(_fold(query)))


def carries(text: str, answer: str) -> bool:
    need = answer_tokens(answer)
    return bool(need) and need <= tokens(text)
