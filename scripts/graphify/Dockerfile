# graphify knowledge-graph engine, containerized.
# Keeps the Python package + tree-sitter grammars off the host (AV + isolation).
# The repo is bind-mounted at /work; `graphify update .` writes graphify-out/ back
# into the mount, where the host skill copies artifacts into .planning/graphs/.
FROM python:3.12-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends git \
    && rm -rf /var/lib/apt/lists/*

# graphifyy is the canonical PyPI package (gsd-core resolves importlib.metadata
# version("graphifyy")). `graphify install` fetches the tree-sitter grammars so
# they bake into the image rather than downloading on every run.
RUN pip install --no-cache-dir graphifyy \
    && graphify install

# Bind-mounted repos are owned by the host user; trust them inside the container.
RUN git config --global --add safe.directory '*'

WORKDIR /work
ENTRYPOINT ["graphify"]
