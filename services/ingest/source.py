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

from aiobotocore.session import get_session
from botocore.session import get_session as sync_get_session
from cocoindex.connectors import amazon_s3
from cocoindex.resources.file import PatternFilePathMatcher

_DEFAULT_ENDPOINT = "http://aura-garage:3900"
_DEFAULT_REGION = "garage"

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
