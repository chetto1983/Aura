"""The S3/Garage source binding: one identity's bucket, nothing else.

"One CocoIndex app per identity" (plan §Self-Review, "The source decision") means the
isolation boundary IS the credential this module is handed -- it never derives, mints,
or looks up a tenant's key itself. That minting is Go-side provisioning (deferred to the
compose/provisioning wiring that launches this container per identity); this module only
reads the already-resolved values from its own environment.

Garage is S3-compatible; amazon_s3.list_objects takes a client we build, so the custom
endpoint lives here and the connector itself needs no knowledge of Garage.
"""

from __future__ import annotations

import dataclasses
import os
import pathlib
import re
import urllib.parse

from aiobotocore.session import get_session
from botocore.session import get_session as sync_get_session
from cocoindex.connectors import amazon_s3
from cocoindex.resources.file import PatternFilePathMatcher

_DEFAULT_ENDPOINT = "http://aura-garage:3900"
_DEFAULT_REGION = "garage"

# The user-metadata entry carrying an object's real filename. Written by Go's
# objectstore.PlaceAsset (const MetadataFileName); every S3 client lowercases the key and
# strips the "x-amz-meta-" prefix, so this bare form is what both sides see.
FILE_NAME_METADATA_KEY = "filename"

_ESCAPES_WELL_FORMED = re.compile(r"(?:[^%]|%[0-9A-Fa-f]{2})*")

# Parts of the bucket that belong to Aura, not to the person whose documents these are.
# MIRRORS internal/assets/browser.go's reservedPrefixes, which hides the same two from the
# file manager; the lists are in different languages and must be changed together.
#
# Reconciling them was actively wrong, not merely wasteful. "identity/" is the asset
# service's own layout, so every chat attachment was indexed a SECOND time under a second
# search_document_id -- the same document twice in one search. And those objects are named
# "original" with no extension, so the temp file got no suffix and Tika's OOXMLParser threw
# "Unexpected RuntimeException" on each one: the errors that made a working pipeline look
# broken were entirely about files the user never uploaded here.
RESERVED_PATTERNS = ("identity/**", "share/**")


@dataclasses.dataclass(frozen=True, slots=True)
class S3Config:
    identity_id: str
    endpoint: str
    bucket: str
    access_key: str
    secret_key: str
    region: str
    prefix: str


def _required(name: str) -> str:
    value = os.environ.get(name, "").strip()
    if not value:
        raise RuntimeError(f"{name} is required")
    return value


def config_from_env() -> S3Config:
    """Read one identity's bucket binding from the process environment.

    AURA_INGEST_IDENTITY_ID also scopes the app name and every passage's
    search_document_id (see app.py) -- it is not just a label here.
    """
    return S3Config(
        identity_id=_required("AURA_INGEST_IDENTITY_ID"),
        endpoint=os.environ.get("AURA_INGEST_S3_ENDPOINT", _DEFAULT_ENDPOINT),
        bucket=_required("AURA_INGEST_S3_BUCKET"),
        access_key=_required("AURA_INGEST_S3_ACCESS_KEY_ID"),
        secret_key=_required("AURA_INGEST_S3_SECRET_ACCESS_KEY"),
        region=os.environ.get("AURA_INGEST_S3_REGION", _DEFAULT_REGION),
        prefix=os.environ.get("AURA_INGEST_S3_PREFIX", ""),
    )


def create_client(config: S3Config):
    """Async context manager yielding the aiobotocore client amazon_s3 needs."""
    session = get_session()
    return session.create_client(
        "s3",
        endpoint_url=config.endpoint,
        aws_access_key_id=config.access_key,
        aws_secret_access_key=config.secret_key,
        region_name=config.region,
    )


def walk(client: object, config: S3Config) -> amazon_s3.S3Walker:
    return amazon_s3.list_objects(
        client,
        config.bucket,
        prefix=config.prefix,
        path_matcher=PatternFilePathMatcher(excluded_patterns=list(RESERVED_PATTERNS)),
    )


def decode_file_name(encoded: str | None) -> str | None:
    """The object's real filename, or None when it does not carry one we can trust.

    A chat attachment's key is `chat/<assetID>.pdf` deliberately -- a key travels into
    presigned URLs and access logs, so putting "Contratto riservato.pdf" there would leak
    the subject to anyone who sees a link. The name rides in user metadata instead, and
    this undoes Go's url.PathEscape.

    unquote, never unquote_plus: Go's encoder never emits '+', so a '+' here is a literal
    one from the operator's filename and turning it into a space would rename their file.

    None rather than a guess, in three cases that all mean "do not trust this":
      - no metadata at all, which is every object uploaded before this channel existed;
      - a value that does not survive a round trip, i.e. a corrupt escape -- unquote is
        lenient and would otherwise hand back a name containing a literal '%zz';
      - anything that decodes to a path. This name builds a temp file and is shown to the
        operator, so a '../' from an object must not travel further.
    """
    if encoded is None:
        return None
    encoded = encoded.strip()
    if not encoded:
        return None
    # Every '%' must introduce a real escape. Checked here rather than by re-encoding and
    # comparing: that would also reject a value escaped MORE than Go escapes it -- '%2B' is
    # a perfectly valid '+' -- and dropping a correctly encoded name is the failure this
    # function exists to avoid.
    if not _ESCAPES_WELL_FORMED.fullmatch(encoded):
        return None
    try:
        decoded = urllib.parse.unquote(encoded, errors="strict")
    except UnicodeDecodeError:
        # Well-formed escapes that are not valid UTF-8: a name we cannot render.
        return None
    decoded = decoded.strip()
    if decoded in ("", ".", ".."):
        return None
    if any(ch in decoded for ch in ("/", "\\", "\x00")):
        return None
    return decoded


async def object_file_name(client: object, config: S3Config, key: str) -> str | None:
    """HEAD one object and return the filename it carries, or None.

    The connector cannot answer this: `amazon_s3.__all__` is
    ('S3File','S3FilePath','S3Walker','get_object','list_objects','read') and S3File exposes
    only content_fingerprint/file_path/read/read_text/size -- there is no metadata surface
    on either the walker or get_object. So this is one direct call on the aiobotocore client
    the app already builds, exactly as expected_keys() already reaches past the connector for
    list_objects_v2.

    Never raises: a missing object or a store that does not serve metadata degrades to the
    key-derived name, which is what the caller did for every object before this existed.
    """
    try:
        head = await client.head_object(Bucket=config.bucket, Key=key)  # type: ignore[attr-defined]
    except Exception:  # noqa: BLE001 - any failure here means "no name", never "no document"
        return None
    return decode_file_name((head.get("Metadata") or {}).get(FILE_NAME_METADATA_KEY))


def expected_keys(config: S3Config) -> set[str]:
    """The object keys a completed pass must have produced a document row for.

    Same bucket, same prefix and the same reserved-prefix exclusion as walk(), because
    the point is to compare against what walk() fed the pipeline -- a different filter
    here would manufacture a discrepancy or hide a real one.

    Synchronous botocore rather than the aiobotocore client above: this runs after the
    pass has finished and its event loop is gone.
    """
    matcher = PatternFilePathMatcher(excluded_patterns=list(RESERVED_PATTERNS))
    client = sync_get_session().create_client(
        "s3",
        endpoint_url=config.endpoint,
        aws_access_key_id=config.access_key,
        aws_secret_access_key=config.secret_key,
        region_name=config.region,
    )
    keys: set[str] = set()
    for page in client.get_paginator("list_objects_v2").paginate(
        Bucket=config.bucket, Prefix=config.prefix
    ):
        for obj in page.get("Contents", []):
            # PurePosixPath, not the raw string: is_file_included calls .as_posix() on
            # what it is handed, so a str raises AttributeError on the first object.
            if matcher.is_file_included(pathlib.PurePosixPath(obj["Key"])):
                keys.add(obj["Key"])
    return keys
