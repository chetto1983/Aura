"""A document's text hash must never claim that two files with no text are the same."""

import hashlib

from ingest.app import _text_fingerprint

# sha256 of the empty string. Named so the assertion below reads as the rule it enforces.
EMPTY_SHA256 = hashlib.sha256(b"").hexdigest()


def test_no_text_yields_no_fingerprint():
    assert _text_fingerprint("") == ""
    assert _text_fingerprint("") != EMPTY_SHA256


def test_text_yields_its_own_hash():
    assert _text_fingerprint("ciao") == hashlib.sha256(b"ciao").hexdigest()


def test_identical_text_hashes_identically():
    assert _text_fingerprint("stesso testo") == _text_fingerprint("stesso testo")
    assert _text_fingerprint("uno") != _text_fingerprint("due")
