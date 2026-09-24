"""One catch-up pass of the production indexing path, for test_embed_space_reruns.py.

    python -m ingest.tests.space_driver <database> <name>=<text> ...

Production reads each object from Garage; here the objects come from argv, and that is the
only part replaced. _extract, chunking, embed.embed_text with the space as its dependency,
and the stamped rows the ArcadeDB target writes are production code. The embedding server is
fake_embedder, so the test can count requests and refuse one input on purpose.
"""

import sys

import cocoindex as coco
from cocoindex.connectors import neo4j

from ingest import app, arcade, embed
from ingest.tests import fake_embedder

_database = ""


@coco.lifespan
async def _lifespan(builder: coco.EnvironmentBuilder):
    # Replaces app.py's lifespan (CocoIndex keeps the last one registered): same target, a
    # disposable database, and no S3 client, since nothing here reads the bucket.
    builder.provide(app.KG_DB, neo4j.ConnectionFactory(
        uri=app.ARCADE_BOLT, auth=("root", app.ARCADE_PASSWORD), database=_database))
    yield


@coco.fn(memo=True)
async def process_object(
    item: tuple[str, str], table: neo4j.TableTarget[app.Passage],
    documents: neo4j.TableTarget[app.IndexedDocument],
) -> None:
    name, text = item
    content = text.encode("utf-8")
    extracted = app._extract(content, ".txt", name, "text/plain")
    await app.index_object(app._S3_CONFIG.identity_id, name, name, content, extracted, table, documents)


@coco.fn
async def driver_main(objects: dict[str, str]) -> None:
    table, documents = await app.mount_targets()
    await coco.mount_each(
        process_object, [(name, (name, text)) for name, text in objects.items()], table, documents,
    )


if __name__ == "__main__":
    _database = sys.argv[1]
    objects = dict(arg.split("=", 1) for arg in sys.argv[2:])
    fake_embedder.install()
    arcade.ensure_schema(app.ARCADE_HTTP, _database, ("root", app.ARCADE_PASSWORD), embed.DIMENSIONS)
    coco.App(coco.AppConfig(name="space-driver"), driver_main, objects).update_blocking()
    print(f"REQUESTS {fake_embedder.requests()}", flush=True)
