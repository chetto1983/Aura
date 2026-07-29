"""In-memory graph fixtures shared by memory-update tests."""

import json

from neo4j_agent_memory.graph import queries


def entity_row(
    entity_id,
    *,
    name,
    canonical_name=None,
    entity_type="PERSON",
    subtype=None,
    description=None,
    metadata=None,
    embedding=None,
):
    return {
        "id": str(entity_id),
        "name": name,
        "canonical_name": canonical_name if canonical_name is not None else name,
        "type": entity_type,
        "subtype": subtype,
        "description": description,
        "embedding": embedding,
        "confidence": 1.0,
        "metadata": json.dumps(metadata) if metadata else None,
        "created_at": None,
    }


def preference_row(
    pref_id,
    *,
    category,
    preference,
    context=None,
    confidence=1.0,
    metadata=None,
    embedding=None,
):
    return {
        "id": str(pref_id),
        "category": category,
        "preference": preference,
        "context": context,
        "confidence": confidence,
        "embedding": embedding,
        "metadata": json.dumps(metadata) if metadata else None,
        "created_at": None,
    }


def fact_row(
    fact_id,
    *,
    subject,
    predicate,
    obj,
    confidence=1.0,
    metadata=None,
    embedding=None,
):
    return {
        "id": str(fact_id),
        "subject": subject,
        "predicate": predicate,
        "object": obj,
        "confidence": confidence,
        "embedding": embedding,
        "valid_from": None,
        "valid_until": None,
        "metadata": json.dumps(metadata) if metadata else None,
        "created_at": None,
    }


class FakeGraphClient:
    """In-memory Neo4jClient covering entity, preference, and fact updates."""

    def __init__(
        self,
        entities=None,
        entity_owned_ids=(),
        preferences=None,
        preference_owned_ids=(),
        facts=None,
        fact_owned_ids=(),
    ):
        self._entities = {str(k): dict(v) for k, v in (entities or {}).items()}
        self._entity_owned_ids = {str(i) for i in entity_owned_ids}
        self._preferences = {str(k): dict(v) for k, v in (preferences or {}).items()}
        self._preference_owned_ids = {str(i) for i in preference_owned_ids}
        self._facts = {str(k): dict(v) for k, v in (facts or {}).items()}
        self._fact_owned_ids = {str(i) for i in fact_owned_ids}
        self.reads: list[tuple[str, dict]] = []
        self.writes: list[tuple[str, dict]] = []

    async def execute_write_unit(self, work):
        return await work()

    async def execute_read(self, query, params=None):
        params = params or {}
        self.reads.append((query, params))

        if query == queries.ENTITY_IN_USER_SCOPE:
            entity_id = params["entity_id"]
            if entity_id not in self._entities:
                return []
            return [{"scope_count": 1 if entity_id in self._entity_owned_ids else 0}]
        if query == queries.GET_ENTITY:
            entity = self._entities.get(params["id"])
            return [{"e": dict(entity)}] if entity else []

        if query == queries.GET_PREFERENCE_SCOPED:
            pref_id = params["preference_id"]
            if pref_id not in self._preferences or pref_id not in self._preference_owned_ids:
                return []
            return [{"p": dict(self._preferences[pref_id])}]

        if query == queries.GET_FACT_SCOPED:
            fact_id = params["fact_id"]
            if fact_id not in self._facts or fact_id not in self._fact_owned_ids:
                return []
            return [{"f": dict(self._facts[fact_id])}]

        raise AssertionError(f"unexpected read query:\n{query}")

    async def execute_write(self, query, params=None):
        params = params or {}
        self.writes.append((query, params))

        if query == queries.UPDATE_ENTITY_FIELDS:
            entity = self._entities[params["id"]]
            for key in ("name", "canonical_name", "description", "subtype", "metadata"):
                if params.get(key) is not None:
                    entity[key] = params[key]
            return [{"e": dict(entity)}]
        if query == queries.UPDATE_ENTITY_EMBEDDING:
            entity = self._entities[params["id"]]
            entity["embedding"] = params["embedding"]
            return [{"e": dict(entity)}]

        if query == queries.UPDATE_PREFERENCE_FIELDS_SCOPED:
            pref_id = params["preference_id"]
            if pref_id not in self._preferences or pref_id not in self._preference_owned_ids:
                return []
            preference = self._preferences[pref_id]
            for key in ("preference", "category", "context", "confidence", "metadata"):
                if params.get(key) is not None:
                    preference[key] = params[key]
            return [{"p": dict(preference)}]
        if query == queries.UPDATE_PREFERENCE_EMBEDDING:
            preference = self._preferences[params["id"]]
            preference["embedding"] = params["embedding"]
            return [{"p": dict(preference)}]

        if query == queries.UPDATE_FACT_FIELDS_SCOPED:
            fact_id = params["fact_id"]
            if fact_id not in self._facts or fact_id not in self._fact_owned_ids:
                return []
            fact = self._facts[fact_id]
            for key in ("subject", "predicate", "object", "confidence", "metadata"):
                if params.get(key) is not None:
                    fact[key] = params[key]
            return [{"f": dict(fact)}]
        if query == queries.UPDATE_FACT_EMBEDDING:
            fact = self._facts[params["id"]]
            fact["embedding"] = params["embedding"]
            return [{"f": dict(fact)}]

        raise AssertionError(f"unexpected write query:\n{query}")


class FakeEmbedder:
    """Record requested texts and return deterministic vectors."""

    def __init__(self):
        self.calls: list[str] = []

    @property
    def dimensions(self) -> int:
        return 3

    async def embed(self, text: str) -> list[float]:
        self.calls.append(text)
        return [float(len(text)), 0.0, 0.0]

    async def embed_batch(self, texts: list[str]) -> list[list[float]]:
        return [await self.embed(text) for text in texts]


class StubMemoryClient:
    """Minimal stand-in for MemoryClient's long-term and graph surfaces."""

    def __init__(self, long_term):
        self.long_term = long_term
        self.graph = long_term._client
