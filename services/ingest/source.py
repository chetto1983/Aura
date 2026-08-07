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

from aiobotocore.session import get_session
from cocoindex.connectors import amazon_s3

_DEFAULT_ENDPOINT = "http://aura-garage:3900"
_DEFAULT_REGION = "garage"


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
    return amazon_s3.list_objects(client, config.bucket, prefix=config.prefix)
