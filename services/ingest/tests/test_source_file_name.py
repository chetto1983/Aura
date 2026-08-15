"""The name an object carries, and what happens when it carries none.

A chat attachment's key is `chat/<assetID>.pdf` on purpose: a key travels into presigned
URLs and access logs, so the filename is deliberately left out of it (Go's
objectstore.AssetKey says why). Until the Go side started writing user metadata, this
sidecar derived the name FROM the key, so every attachment reached the index -- and the
operator's search -- as `019f8a2b-....pdf`.

decode_file_name is the exact inverse of Go's objectstore.encodeFileName (url.PathEscape),
and these tests are the contract between the two languages.
"""

import pytest

from ingest import source


@pytest.mark.parametrize(
    "name",
    [
        "Contratto ACME 2026.pdf",
        "Perizia città di Ghèdi — 2026.pdf",
        "Отчёт.pdf",
        "Relazione (bozza) - v2 [final].pdf",
        "già+così&poi=fine.txt",
        "\"virgolette\" e 'apici'.md",
    ],
)
def test_decodes_what_go_encoded(name):
    # urllib.parse.quote(safe=...) mirrors Go's url.PathEscape: both leave the RFC 3986
    # sub-delims alone and percent-encode every non-ASCII byte. If either side ever drifts,
    # this is where it shows.
    from urllib.parse import quote

    assert source.decode_file_name(quote(name, safe="$&+,/:;=?@")) == name


def test_a_plus_is_a_plus_not_a_space():
    """unquote_plus would turn this into a space and rename the operator's file."""
    assert source.decode_file_name("rev%2B1.pdf") == "rev+1.pdf"


@pytest.mark.parametrize(
    "value",
    [
        None,  # object written before the metadata channel existed
        "",
        "   ",
        "..%2F..%2Fetc%2Fpasswd",  # the name builds a temp file path
        "%2Fetc%2Fpasswd",
        "%2F",
        "a%00b.pdf",
        ".",
        "..",
    ],
)
def test_refuses_what_it_cannot_trust(value):
    """None, not a guess: the caller falls back to the key, which is at least honest."""
    assert source.decode_file_name(value) is None


def test_undecodable_percent_escape_is_refused():
    # Python's unquote is lenient -- it leaves a bad escape as literal text rather than
    # raising -- so a value that does not round-trip is rejected explicitly. Otherwise a
    # corrupt header would silently become a filename containing '%zz'.
    assert source.decode_file_name("%zz%zz") is None
