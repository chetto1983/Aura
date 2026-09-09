"""The card must be scorable on the same scale as a passage.

Today the card leg is BM25 over IndexedDocument[card] while the passage leg is a reranked
cosine, and the two cannot be compared. rankDocuments therefore falls back to a precedence
rule -- anything with a passage outranks anything without -- and a document with no passages
becomes unreachable: measured 2026-09-09, gi_comuni_cap.xlsx did not come back even when
searched by its exact filename. Embedding the card is what puts both legs on one scale.
"""
from ingest import app, arcade


def test_the_document_type_carries_a_vector_index_for_its_card():
    ddl = " ".join(arcade._document_ddl(768))

    assert "CREATE PROPERTY IndexedDocument.embedding IF NOT EXISTS ARRAY_OF_FLOATS" in ddl
    assert "ON IndexedDocument (embedding) LSM_VECTOR" in ddl
    assert '"dimensions": 768' in ddl


def test_the_indexed_document_record_carries_the_embedding():
    fields = {f.name for f in app.dataclasses.fields(app.IndexedDocument)}

    assert "embedding" in fields
