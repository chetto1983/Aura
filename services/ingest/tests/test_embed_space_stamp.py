"""Every document vector carries the space that produced it (spec §2).

The documents gate compares these stamps with the reader's space, so a row without one, or
with an index that cannot see a missing one, would let dense retrieval open over vectors
nobody checked.
"""

import dataclasses

from ingest import app, arcade


def test_both_document_types_declare_the_stamp_and_index_its_nulls():
    ddl = arcade._document_ddl(768)

    for type_name in (arcade.PASSAGE_TYPE, arcade.DOCUMENT_TYPE):
        assert f"CREATE PROPERTY {type_name}.embed_space IF NOT EXISTS STRING" in ddl
        assert f"CREATE INDEX IF NOT EXISTS ON {type_name} (embed_space) NOTUNIQUE NULL_STRATEGY INDEX" in ddl


def test_every_declared_row_carries_a_stamp():
    for record in (app.Passage, app.IndexedDocument):
        assert "embed_space" in {field.name for field in dataclasses.fields(record)}
