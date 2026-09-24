# MCP binary files: WhatsApp fork implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `download_media` returns a `resource_link` to `whatsapp-media://{chat_jid}/{message_id}`. Any MCP client, Aura included, reads the media's bytes back through `resources/read` on its own authenticated session. The fork also moves to current dependencies and a CI that tests what it ships.

**Architecture:**

- **One new module, `media_files.py`, holds the media logic.** It knows a received file's name, MIME type and size, builds the tool result, and serves the resource. The bridge stores no MIME type, saves every image as `.jpg`, and drops a document's own name, so the module reads the message row (`get_media_meta`) and the file's first bytes.
- **`main.py` stays thin.** `download_media` and the new `read_media` resource are one-line delegations.
- **Tenant binding.** `SubjectTenantMiddleware` binds the tenant for `resources/read` as well as `tools/call`.
- **Validation.** Measured on 2026-09-24 in a WSL copy on the upgraded lock:
  - a `resources/read` through `mcp.Client(main.mcp)` crosses the middleware, as method `resources/read`;
  - today the handler then runs under the suite's default tenant, not the caller's;
  - a `ResourceNotFoundError` / `ResourceError` reaches the client as `MCPError` carrying the server's message;
  - a tool annotated `-> CallToolResult` has no output schema and no structured content;
  - the dependency upgrade passed 114/114 tests, ruff, `pip-audit` (0 findings), `govulncheck` on go1.27.1 (0 reachable) and golangci-lint v2.13.2 (0 issues).

**Tech Stack:** Python 3.11, `mcp` 2.2.0 (`MCPServer`, `mcp.Client`), pytest + pytest-asyncio, ruff, uv 0.12.18. Go 1.27.1 bridge (whatsmeow).

**Spec:** `D:\Aura\docs\superpowers\specs\2026-09-24-mcp-binary-files-design.md`, section "WhatsApp fork (`whatsapp-mcp`)".

## Global Constraints

**Repository and commits**
- Repository: `D:\tmp\aura-whatsapp-mcp`, branch `main`. Local `main` equals `origin/main` (`6aaa87d`) and the tree is clean. Commit on `main`, and do not push until Task 4.
- Commit titles follow Conventional Commits, as `AGENTS.md` requires. Commit with explicit paths (`git commit -- <paths>`). End every message with `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`. Never use `--no-verify`.
- `AGENTS.md` forbids editing `CHANGELOG.md`, `.release-please-manifest.json` and the project `version`, because release-please owns them.
- Pushing `main` publishes `ghcr.io/chetto1983/whatsapp-mcp:latest` through `.github/workflows/publish-image.yml`, and every appliance's updater refreshes to that tag. **The push needs the operator's explicit go.**

**Build and test**
- Build and test in WSL through the scratchpad script `wa_build.sh`, created in Task 1. It mirrors the fork to `~/wa-build`, normalizes CRLF to LF there, and runs `deps`, `test` or `audit`.
- Never run a Windows `.exe`. There is no docker CLI in this WSL distro, so the image is built in CI only (Task 4).
- The Windows checkout uses CRLF line endings (`core.autocrlf=true`). Edit it with the Edit tool, never `sed`. The generated files (`go.mod`, `go.sum`, `uv.lock`) are written in the mirror by the tools and copied back.

**Values from the spec**
- Per-file cap: **25 MiB** (`25 * 1024 * 1024`).
- Resource URI template: `whatsapp-media://{chat_jid}/{message_id}`, registered with MIME type `application/octet-stream`. The link carries the real type.
- Tool text JSON keys: `success`, `message`, `name`, `mime_type`, `size_bytes`, `file_path`.
- Sniff table: JPEG, PNG, WebP, GIF, PDF, OGG, MP4. Anything unrecognised is `application/octet-stream`.
- Bridge download timeout: **70 s**. The tenant gateway waits 65 s (`tenant_gateway.py:160`), and the bridge's write timeout is 60 s.
- Toolchain: `go 1.27.1` in `go.mod` and `golang:1.27.1-bookworm` in the Dockerfile. `uv==0.12.18` (latest on PyPI, checked 2026-09-24). golangci-lint `v2.13.2`.

**Unchanged contracts**
- `get_media_data`: its name, arguments, `dict` shape and the `whatsapp_actions.download_media` hook are the MCP Apps view's contract (`ui/client.html`, `payloadOf` in `ui/_bridge.js`).

## Review Focus

1. **Another tenant's link.** Tenant B reads a URI minted for tenant A. The read must find nothing, and the bridge must never be asked. Pinned in Task 3 (`test_another_tenant_reads_nothing_and_never_reaches_the_bridge`), which runs through the real middleware with a caller who is not the suite's default tenant.
2. **A PNG the bridge saved as `.jpg`.** The name and type must say PNG, because a client decides how to open the file from them. Pinned in Task 2 (`test_an_image_saved_as_jpg_that_is_a_png_is_named_and_typed_png`).
3. **A hostile document name.** A sender names a document `../../etc/passwd.pdf` or a Windows path. Only the base name may reach a client. Pinned in Task 2 (`test_a_document_keeps_the_name_its_sender_gave_it`).
4. **Old media.** WhatsApp expires media keys, so a download routinely fails for old messages. The tool must say so and link nothing. Pinned in Task 3 (`test_a_failed_download_says_so_and_links_nothing`).
5. **Audio from an iPhone.** It arrives in an MP4 container. It must be typed `audio/mp4`, not `video/mp4`. Pinned in Task 2 (the `ftypM4A` row of the sniff table).

---

### Task 1: Current dependencies, Go 1.27.1, and a CI that tests the lock

**Files:**
- Create: the scratchpad script `wa_build.sh`, which is not in the repository
- Modify: `whatsapp-mcp-server/pyproject.toml` (dependency floors)
- Modify: `whatsapp-mcp-server/uv.lock` (regenerated)
- Modify: `whatsapp-bridge/go.mod`, `whatsapp-bridge/go.sum` (regenerated)
- Modify: `whatsapp-mcp-server/mcp_security.py` (`auth_settings`)
- Test: `whatsapp-mcp-server/tests/test_mcp_security.py`
- Modify: `Dockerfile`, `.github/workflows/ci.yml`, `.github/workflows/security.yml`

**Interfaces:**
- Produces:
  - `auth_settings(config)` returns `AuthSettings(..., validate_token_resource=False)`;
  - `wa_build.sh deps|test|audit`, used by every later task.

- [ ] **Step 1: Create the build script.** Write this to `<scratchpad>/wa_build.sh`:

```bash
#!/usr/bin/env bash
# Mirror the WhatsApp fork into WSL-native storage and run one gate there.
#   deps   upgrade the Go module and the uv lock in the mirror, copy them back
#   test   go build + vet + test, then the locked Python suite and ruff
#   audit  govulncheck, golangci-lint, pip-audit over the exported lock
set -uo pipefail
export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"
SRC=/mnt/d/tmp/aura-whatsapp-mcp
DST="$HOME/wa-build"
mkdir -p "$DST"
rsync -a --delete --exclude .git/ --exclude .venv/ --exclude whatsapp-bridge/store/ "$SRC/" "$DST/"
# The Windows checkout is CRLF and git stores LF: test what git stores.
find "$DST" -type f \( -name '*.py' -o -name '*.go' -o -name '*.toml' -o -name '*.lock' -o -name go.mod -o -name go.sum \) \
  -exec sed -i 's/\r$//' {} +
case "${1:-test}" in
deps)
  cd "$DST/whatsapp-bridge" || exit 1
  go mod edit -go=1.27.1
  go get go.mau.fi/whatsmeow@latest go.mau.fi/util@latest github.com/mattn/go-sqlite3@latest \
    google.golang.org/protobuf@latest golang.org/x/crypto@latest golang.org/x/net@latest
  go mod tidy
  cp go.mod go.sum "$SRC/whatsapp-bridge/"
  grep -E '^go |whatsmeow|go-sqlite3|protobuf |x/crypto|x/net|go.mau.fi/util' go.mod
  cd "$DST/whatsapp-mcp-server" || exit 1
  uv lock --upgrade
  cp uv.lock "$SRC/whatsapp-mcp-server/"
  for p in mcp anyio cryptography httpx2; do printf '%s ' "$p"; grep -A1 "^name = \"$p\"$" uv.lock | grep version; done
  ;;
test)
  cd "$DST/whatsapp-bridge" || exit 1
  go version
  CGO_ENABLED=1 go build ./... && go vet ./... && CGO_ENABLED=1 go test ./...
  echo "go-exit=$?"
  cd "$DST/whatsapp-mcp-server" || exit 1
  uv run --frozen --extra dev pytest -q 2>&1 | tail -20
  uv run --frozen --extra dev ruff check .
  uv run --frozen --extra dev ruff format --check .
  ;;
audit)
  cd "$DST/whatsapp-bridge" || exit 1
  govulncheck ./...
  golangci-lint run ./...
  cd "$DST/whatsapp-mcp-server" || exit 1
  uv export --frozen --no-hashes --no-dev -o "$DST/requirements-audit.txt" >/dev/null
  uvx --quiet pip-audit -r "$DST/requirements-audit.txt" --no-deps --disable-pip
  ;;
esac
```

Run it as `MSYS_NO_PATHCONV=1 wsl bash /mnt/c/Users/Davide/AppData/Local/Temp/claude/d--Aura/<session>/scratchpad/wa_build.sh <mode>`, where `<session>` is this session's scratchpad folder.

- [ ] **Step 2: Raise the Python floors.** In `whatsapp-mcp-server/pyproject.toml`, replace:

```toml
    # mcp 2.0.0 is the SDK for the 2026-07-28 revision: stateless core, the
    # extensions framework, and MCP Apps in `mcp.server.apps`. It removed
    # `mcp.server.fastmcp` outright, so this floor is a requirement, not a
    # preference. The SDK provides its own `httpx2` transport client.
    "mcp>=2.0.0",
    "pyjwt[crypto]>=2.13.0",
    "requests>=2.34.2",
    "anyio<4.14",
```

with:

```toml
    # mcp 2.0.0 is the SDK for the 2026-07-28 revision: stateless core, the
    # extensions framework, and MCP Apps in `mcp.server.apps`. It removed
    # `mcp.server.fastmcp` outright. 2.2.0 is the release this server's lock was
    # audited on (2026-09-24, no known vulnerabilities). The SDK provides its
    # own `httpx2` transport client.
    "mcp>=2.2.0",
    "pyjwt[crypto]>=2.13.0",
    "requests>=2.34.2",
    # 4.14.2 fixes CVE-2026-63374 and CVE-2026-64847. The old `<4.14` cap came
    # from a dependabot bump, not from an incompatibility.
    "anyio>=4.14.2",
```

- [ ] **Step 3: Upgrade the lock and the Go module.** Run `wa_build.sh deps`.

Expected:
- `go 1.27.1`;
- whatsmeow at `v0.0.0-20260921121126-…` or later, go-sqlite3 `v1.14.52` or later, protobuf `v1.36.12` or later, x/crypto `v0.57.0` or later, x/net `v0.59.0` or later;
- mcp `2.2.0`, anyio `4.15.1` or later, cryptography `50.0.1` or later, httpx2 `2.13.1` or later.

- [ ] **Step 4: Run the suites on the new set.** Run `wa_build.sh test`.

Expected: `go-exit=0`, `114 passed`, ruff `All checks passed!`, and `26 files already formatted`.

- [ ] **Step 5: Write the failing test.** In `whatsapp-mcp-server/tests/test_mcp_security.py`, add `import warnings` between `import time` and `from uuid import UUID`. Then add this test directly after `test_auth_settings_publish_generic_resource_contract`:

```python
def test_auth_settings_leave_the_audience_check_to_the_verifier():
    """JWTTokenVerifier checks the token's audience against every name this server
    answers to (`accepted_audiences`); the SDK's own check knows only
    `resource_server_url`. mcp 2.2 warns until the choice is made explicitly."""
    config = OAuthConfig(
        issuers=(TrustedIssuer(issuer=ISSUER, jwks_url="https://auth.example/jwks"),), resource=RESOURCE
    )

    with warnings.catch_warnings():
        warnings.simplefilter("error")
        settings = auth_settings(config)

    assert settings.validate_token_resource is False
```

- [ ] **Step 6: Run it to verify it fails.** Run `wa_build.sh test`.

Expected: FAIL in `test_auth_settings_leave_the_audience_check_to_the_verifier` with `MCPDeprecationWarning: AuthSettings.validate_token_resource is not set`.

- [ ] **Step 7: Implement.** In `whatsapp-mcp-server/mcp_security.py`, replace the body of `auth_settings`:

```python
def auth_settings(config: OAuthConfig) -> AuthSettings:
    return AuthSettings(
        issuer_url=config.home.issuer,
        resource_server_url=config.resource,
        required_scopes=[MCP_TOOLS_SCOPE],
        # JWTTokenVerifier already checks the audience against every accepted name;
        # the SDK's check would compare the token with this one URL only.
        validate_token_resource=False,
    )
```

- [ ] **Step 8: Run the tests.** Run `wa_build.sh test`. Expected: `go-exit=0`, `115 passed`, ruff clean.

- [ ] **Step 9: Pin the image and CI to the same set.**

In `Dockerfile`:
- replace `FROM golang:bookworm AS bridge-build` with `FROM golang:1.27.1-bookworm AS bridge-build`;
- replace `RUN pip install --no-cache-dir uv==0.5.31` with `RUN pip install --no-cache-dir uv==0.12.18`.

In `.github/workflows/ci.yml`, replace the `python-lint` job's install step:

```yaml
      - name: Install dependencies
        run: |
          cd whatsapp-mcp-server
          uv venv
          uv pip install ruff
```

with:

```yaml
      - name: Install the locked dependencies
        run: |
          cd whatsapp-mcp-server
          uv sync --frozen --extra dev
```

and change that job's two run lines to `uv run --frozen --extra dev ruff check .` and `uv run --frozen --extra dev ruff format --check .`.

In the `python-test` job, replace:

```yaml
      - name: Install dependencies
        run: |
          cd whatsapp-mcp-server
          uv venv
          uv pip install -e ".[dev]"
      - name: Run tests
        run: |
          cd whatsapp-mcp-server
          uv run pytest -v
```

with:

```yaml
      - name: Install the locked dependencies
        run: |
          cd whatsapp-mcp-server
          uv sync --frozen --extra dev
      - name: Run tests
        run: |
          cd whatsapp-mcp-server
          uv run --frozen --extra dev pytest -v
```

In the `go-lint` and `go-build` jobs, replace each `go-version: '1.25'` with `go-version-file: whatsapp-bridge/go.mod`. In `go-lint`, change `version: v2.7.1` to `version: v2.13.2`. At the end of `go-build`, after its `Build` step, add:

```yaml
      - name: Test
        run: |
          cd whatsapp-bridge
          go test ./...
```

Keep the job names: branch protection matches on them.

In `.github/workflows/security.yml`:
- In `codeql-go` and `govulncheck`, replace each `go-version: '1.25'` with `go-version-file: whatsapp-bridge/go.mod`.
- In `python-audit`, audit the lock instead of an unlocked install. Replace its steps after `actions/checkout@v7` with:

```yaml
      - uses: astral-sh/setup-uv@v7

      - name: Export the locked dependencies
        run: |
          cd whatsapp-mcp-server
          uv export --frozen --no-hashes --no-dev -o requirements-audit.txt

      - name: Audit dependencies
        run: uvx pip-audit -r whatsapp-mcp-server/requirements-audit.txt --no-deps --disable-pip
        continue-on-error: true
```

- [ ] **Step 10: Audit.** Run `wa_build.sh audit`.

Expected:
- `govulncheck`: `No vulnerabilities found.` Imported-but-unreachable findings are acceptable, and a reachable one is not.
- `golangci-lint`: `0 issues.`
- `pip-audit`: `No known vulnerabilities found`.

- [ ] **Step 11: Commit.**

```bash
cd /d/tmp/aura-whatsapp-mcp
git commit -F - -- whatsapp-mcp-server/pyproject.toml whatsapp-mcp-server/uv.lock whatsapp-bridge/go.mod whatsapp-bridge/go.sum whatsapp-mcp-server/mcp_security.py whatsapp-mcp-server/tests/test_mcp_security.py Dockerfile .github/workflows/ci.yml .github/workflows/security.yml <<'EOF'
build: current dependencies, Go 1.27.1, and CI on the locked set

The bridge declared go 1.25.0 and built on a floating golang:bookworm. The
audit's nine reachable stdlib CVEs (GO-2026-4970, an os.Root symlink escape
in the media paths, among them) came from the go1.26.3 toolchain that
directive let a host keep. With go 1.27.1 declared, govulncheck reports none
reachable, and the image is pinned to the same release. whatsmeow,
go.mau.fi/util, go-sqlite3, protobuf, x/crypto and x/net move to their
latest releases.

On the Python side anyio loses a cap that dependabot added and nothing
needed (4.14.2 fixes CVE-2026-63374/64847), and the lock moves to mcp 2.2.0,
cryptography 50 and httpx2 2.13: pip-audit finds nothing. mcp 2.2 warns
unless AuthSettings.validate_token_resource is set; it is False, because
JWTTokenVerifier checks every accepted audience itself.

CI installed Python dependencies unlocked, so it never tested what the image
ships, and it never ran the bridge's Go tests. It now syncs the frozen lock,
runs go test, reads the Go version from go.mod and pins golangci-lint to
v2.13.2; the dependency audit reads the lock as well.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
EOF
```

---

### Task 2: `media_files`: what a received file is called and what it is

**Files:**
- Create: `whatsapp-mcp-server/media_files.py`
- Modify: `whatsapp-mcp-server/pyproject.toml` (`[tool.setuptools] py-modules`)
- Modify: `whatsapp-mcp-server/whatsapp_query_messages.py` (append `get_media_meta`, import `closing`)
- Modify: `whatsapp-mcp-server/whatsapp_actions.py` (`download_media`: timeout)
- Modify: `whatsapp-mcp-server/tests/conftest.py` (fixtures `seed_media`, `bridge_download`)
- Test: `whatsapp-mcp-server/tests/test_media_files.py`

**Interfaces:**
- Produces:
  - `get_media_meta(message_id: str, chat_jid: str) -> tuple[str | None, str | None] | None` in `whatsapp_query_messages`;
  - in `media_files`:
    - `OCTET_STREAM`, `MAX_MEDIA_FILE_BYTES`, `MEDIA_URI_TEMPLATE`;
    - `MediaFile(path: str, name: str, mime_type: str, size_bytes: int)`, a frozen dataclass;
    - `sniff(head: bytes, media_type: str | None) -> tuple[str, str]`, returning `(mime, ext)`;
    - `describe(media_type, filename, message_id, head) -> tuple[str, str]`, returning `(name, mime)`;
    - `media_uri(chat_jid: str, message_id: str) -> str | None`;
    - `media_file(message_id: str, chat_jid: str) -> MediaFile | None`, which raises `OSError` when the downloaded file is unreadable;
  - test fixtures:
    - `seed_media(identity, message_id, chat_jid, media_type, filename=None)`;
    - `bridge_download(blob: bytes | None) -> list[tuple[str, str]]`, returning the requests it received.

- [ ] **Step 1: Add the fixtures.** Replace `whatsapp-mcp-server/tests/conftest.py` with:

```python
import sqlite3
from contextlib import closing

import pytest

import whatsapp_actions
from tenant_context import tenant_scope, tenant_store

TEST_IDENTITY = "11111111-1111-4111-8111-111111111111"
TEST_BRIDGE_TOKEN = "bridge-token-for-tests"


@pytest.fixture(autouse=True)
def tenant_context(tmp_path, monkeypatch):
    monkeypatch.setenv("WHATSAPP_STORE_ROOT", str(tmp_path / "whatsapp-root"))
    monkeypatch.setenv("WHATSAPP_BRIDGE_TOKEN", TEST_BRIDGE_TOKEN)
    with tenant_scope(TEST_IDENTITY):
        yield


@pytest.fixture
def seed_media():
    """Write one message row into a tenant's messages.db, the columns the bridge fills."""

    def seed(identity: str, message_id: str, chat_jid: str, media_type: str | None, filename: str | None = None):
        path = tenant_store(identity) / "messages.db"
        path.parent.mkdir(parents=True, exist_ok=True)
        with closing(sqlite3.connect(path)) as conn, conn:
            conn.execute(
                "CREATE TABLE IF NOT EXISTS messages ("
                "id TEXT, chat_jid TEXT, sender TEXT, content TEXT, timestamp TIMESTAMP, is_from_me BOOLEAN, "
                "media_type TEXT, filename TEXT, PRIMARY KEY (id, chat_jid))"
            )
            conn.execute(
                "INSERT INTO messages VALUES (?, ?, ?, '', '2026-09-24 10:00:00', 0, ?, ?)",
                (message_id, chat_jid, chat_jid, media_type, filename),
            )

    return seed


@pytest.fixture
def bridge_download(tmp_path, monkeypatch):
    """Stand in for the bridge's /api/download. `place(blob)` makes it answer with a
    file holding `blob`, named `.jpg` the way the bridge names every image; None
    makes the download fail. Returns the list of requests it receives."""
    calls: list[tuple[str, str]] = []

    def place(blob: bytes | None) -> list[tuple[str, str]]:
        path = tmp_path / "bridge-download.jpg"
        if blob is not None:
            path.write_bytes(blob)

        def download(message_id: str, chat_jid: str) -> str | None:
            calls.append((message_id, chat_jid))
            return str(path) if blob is not None else None

        monkeypatch.setattr(whatsapp_actions, "download_media", download)
        return calls

    return place
```

- [ ] **Step 2: Write the failing tests.** Create `whatsapp-mcp-server/tests/test_media_files.py`:

```python
"""What download_media reports about a received file, and what it refuses to guess.

The bridge stores no MIME type, writes every image as .jpg and drops a document's
own name, so the name and the type come from the message row and the file's first
bytes. Every case here is one a real chat produces.
"""

from pathlib import Path

import pytest

import media_files
import whatsapp_actions
from tenant_context import current_identity

CHAT = "393331234567@s.whatsapp.net"
GROUP = "120363025246125888@g.us"
MSG = "3EB0C767D26A1D3B9A0C"
JPEG = b"\xff\xd8\xff\xe0\x00\x10JFIF\x00"
PNG = b"\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"
DOCX = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
XLSX = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"


@pytest.mark.parametrize(
    ("head", "media_type", "expected"),
    [
        (JPEG, "image", ("image/jpeg", ".jpg")),
        (PNG, "image", ("image/png", ".png")),
        (b"GIF89a\x01\x00", "image", ("image/gif", ".gif")),
        (b"RIFF\x24\x00\x00\x00WEBPVP8 ", "sticker", ("image/webp", ".webp")),
        (b"%PDF-1.7\n%\xe2\xe3", "document", ("application/pdf", ".pdf")),
        (b"OggS\x00\x02\x00\x00", "audio", ("audio/ogg", ".ogg")),
        (b"\x00\x00\x00\x18ftypmp42", "video", ("video/mp4", ".mp4")),
        (b"\x00\x00\x00\x1cftypM4A ", "audio", ("audio/mp4", ".m4a")),
        (b"PK\x03\x04\x14\x00", "document", ("application/octet-stream", "")),
        (b"", "image", ("application/octet-stream", "")),
    ],
)
def test_sniff_types_a_file_by_its_first_bytes(head, media_type, expected):
    assert media_files.sniff(head, media_type) == expected


def test_an_image_saved_as_jpg_that_is_a_png_is_named_and_typed_png():
    assert media_files.describe("image", None, MSG, PNG) == (f"image_{MSG}.png", "image/png")


@pytest.mark.parametrize(
    ("filename", "expected"),
    [
        ("Fattura settembre.pdf", ("Fattura settembre.pdf", "application/pdf")),
        ("Relazione finale.docx", ("Relazione finale.docx", DOCX)),
        ("../../etc/passwd.pdf", ("passwd.pdf", "application/pdf")),
        ("C:\\Users\\anna\\Preventivo.xlsx", ("Preventivo.xlsx", XLSX)),
    ],
)
def test_a_document_keeps_the_name_its_sender_gave_it(filename, expected):
    assert media_files.describe("document", filename, MSG, b"PK\x03\x04") == expected


@pytest.mark.parametrize("filename", [None, "", ".", ".."])
def test_a_document_without_a_usable_name_is_named_like_other_media(filename):
    assert media_files.describe("document", filename, MSG, b"%PDF-1.4") == (f"document_{MSG}.pdf", "application/pdf")


def test_a_document_whose_name_has_no_known_type_is_typed_by_its_bytes():
    assert media_files.describe("document", "scansione", MSG, b"%PDF-1.4") == ("scansione", "application/pdf")


def test_a_hostile_message_id_cannot_shape_the_file_name():
    name, _ = media_files.describe("image", None, "../x/y", JPEG)

    assert "/" not in name
    assert ".." not in name


def test_the_uri_carries_the_chat_and_the_message():
    assert media_files.media_uri(GROUP, MSG) == f"whatsapp-media://{GROUP}/{MSG}"


@pytest.mark.parametrize(
    ("chat_jid", "message_id"),
    [(CHAT, ""), ("", MSG), (CHAT, "a/b"), (CHAT, "a%2Fb"), (CHAT, "a?b"), (CHAT, "a#b"), (CHAT, "a b")],
)
def test_an_id_that_cannot_round_trip_gets_no_uri(chat_jid, message_id):
    assert media_files.media_uri(chat_jid, message_id) is None


def test_media_file_describes_what_the_bridge_downloaded(seed_media, bridge_download):
    seed_media(current_identity(), MSG, CHAT, "document", "Fattura.pdf")
    calls = bridge_download(b"%PDF-1.7 body")

    media = media_files.media_file(MSG, CHAT)

    assert calls == [(MSG, CHAT)]
    assert (media.name, media.mime_type, media.size_bytes) == ("Fattura.pdf", "application/pdf", 13)
    assert Path(media.path).read_bytes() == b"%PDF-1.7 body"


def test_a_message_the_tenant_does_not_have_never_reaches_the_bridge(seed_media, bridge_download):
    seed_media(current_identity(), "ANOTHER", CHAT, "image")
    calls = bridge_download(JPEG)

    assert media_files.media_file(MSG, CHAT) is None
    assert calls == []


@pytest.mark.parametrize("media_type", ["reaction", "", None])
def test_a_message_without_a_file_never_reaches_the_bridge(seed_media, bridge_download, media_type):
    """A reaction row stores the reacted-to message id in `filename`; it is not a file."""
    seed_media(current_identity(), MSG, CHAT, media_type, "3EB0REACTEDTO")
    calls = bridge_download(JPEG)

    assert media_files.media_file(MSG, CHAT) is None
    assert calls == []


def test_a_failed_download_is_none(seed_media, bridge_download):
    seed_media(current_identity(), MSG, CHAT, "image")
    bridge_download(None)

    assert media_files.media_file(MSG, CHAT) is None


def test_a_downloaded_path_that_is_gone_raises(seed_media, monkeypatch, tmp_path):
    seed_media(current_identity(), MSG, CHAT, "image")
    monkeypatch.setattr(whatsapp_actions, "download_media", lambda *_: str(tmp_path / "gone.jpg"))

    with pytest.raises(OSError):
        media_files.media_file(MSG, CHAT)


def test_the_bridge_download_gives_up_after_70_seconds(monkeypatch):
    """The tenant gateway answers within 65 s; without a timeout of its own a
    stuck runtime held the tool call forever."""
    seen = {}

    def post(url, **kwargs):
        seen.update(kwargs)
        raise whatsapp_actions.requests.Timeout("stuck")

    monkeypatch.setattr(whatsapp_actions.requests, "post", post)

    assert whatsapp_actions.download_media(MSG, CHAT) is None
    assert seen["timeout"] == 70
```

- [ ] **Step 3: Run the tests to verify they fail.** Run `wa_build.sh test`.

Expected: collection ERROR in `tests/test_media_files.py` with `ModuleNotFoundError: No module named 'media_files'`.

- [ ] **Step 4: Add the metadata query.** In `whatsapp-mcp-server/whatsapp_query_messages.py`, add `from contextlib import closing` between `import sqlite3` and `from datetime import datetime`. Then append at the end of the file:

```python


def get_media_meta(message_id: str, chat_jid: str) -> tuple[str | None, str | None] | None:
    """A message's `(media_type, filename)` in the active tenant's store, or None when
    the store has no such message. Another tenant's message id finds nothing here."""
    with closing(sqlite3.connect(MESSAGES_DB_PATH)) as conn:
        return conn.execute(
            "SELECT media_type, filename FROM messages WHERE id = ? AND chat_jid = ?",
            (message_id, chat_jid),
        ).fetchone()
```

- [ ] **Step 5: Give the download a timeout.** In `whatsapp-mcp-server/whatsapp_actions.py`, below `WHATSAPP_API_BASE_URL`, add:

```python

# Above the tenant gateway's own 65 s (tenant_gateway.py), so the gateway's answer
# arrives first; the bridge itself gives a media download 60 s to write.
DOWNLOAD_TIMEOUT_SECONDS = 70
```

In `download_media`, replace `response = requests.post(url, json=payload, headers=_bridge_headers())` with `response = requests.post(url, json=payload, headers=_bridge_headers(), timeout=DOWNLOAD_TIMEOUT_SECONDS)`. Change only this call. The send calls upload a file, and their limits are a different question.

- [ ] **Step 6: Implement the module.** Create `whatsapp-mcp-server/media_files.py`:

```python
"""A received file as an MCP client sees it: its name, type and size, and the
`whatsapp-media://` resource its bytes are read back through.

The bridge stores no MIME type, writes every image as `.jpg` (PNGs included) and
drops a document's own name when it saves the file, so none of the three can be
read off the path it returns. A document is named by the name its sender gave it
(`messages.filename`); everything else is typed by its first bytes.
"""

import mimetypes
import os
from dataclasses import dataclass
from pathlib import PurePosixPath

import whatsapp_actions
from whatsapp_query_messages import get_media_meta

OCTET_STREAM = "application/octet-stream"
# The cap Aura's MCP bridge materializes a file under. WhatsApp itself carries far
# larger documents; one over the cap is still linked, with its size, and refused
# on read.
MAX_MEDIA_FILE_BYTES = 25 * 1024 * 1024
# No store path and no tenant in the URI: this process can read every tenant's
# store, so the tenant comes from the caller's token, never from the link.
MEDIA_URI_TEMPLATE = "whatsapp-media://{chat_jid}/{message_id}"

# The media_type values the bridge downloads; "reaction" rows are not files.
_DOWNLOADABLE = frozenset({"image", "video", "audio", "document", "sticker"})
# Characters that would split or re-decode a template segment on the way back.
_URI_UNSAFE = frozenset("/?#&,{}% ")
_MAGIC = (
    (b"\xff\xd8\xff", "image/jpeg", ".jpg"),
    (b"\x89PNG\r\n\x1a\n", "image/png", ".png"),
    (b"GIF8", "image/gif", ".gif"),
    (b"%PDF-", "application/pdf", ".pdf"),
    (b"OggS", "audio/ogg", ".ogg"),
)
_SNIFF_BYTES = 16

# The standard library's own table, not the host's /etc/mime.types: the same name
# must get the same type in the test run and in the slim image. WhatsApp documents
# are mostly PDFs and Office files, and the built-in table lacks OOXML.
_TYPES = mimetypes.MimeTypes()
_TYPES.add_type("application/vnd.openxmlformats-officedocument.wordprocessingml.document", ".docx")
_TYPES.add_type("application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", ".xlsx")
_TYPES.add_type("application/vnd.openxmlformats-officedocument.presentationml.presentation", ".pptx")


@dataclass(frozen=True)
class MediaFile:
    path: str
    name: str
    mime_type: str
    size_bytes: int


def sniff(head: bytes, media_type: str | None) -> tuple[str, str]:
    """The MIME type and extension a file's first bytes declare."""
    if head[:4] == b"RIFF" and head[8:12] == b"WEBP":
        return "image/webp", ".webp"
    if head[4:8] == b"ftyp":
        # One container for both: an audio message in it is AAC from an iPhone.
        return ("audio/mp4", ".m4a") if media_type == "audio" else ("video/mp4", ".mp4")
    for magic, mime, ext in _MAGIC:
        if head.startswith(magic):
            return mime, ext
    return OCTET_STREAM, ""


def describe(media_type: str | None, filename: str | None, message_id: str, head: bytes) -> tuple[str, str]:
    """The name and MIME type a client should see for this file."""
    if media_type == "document":
        # The sender chose this name; only its last segment is a file name.
        name = PurePosixPath((filename or "").replace("\\", "/")).name
        if name not in ("", ".."):
            return name, _TYPES.guess_type(name)[0] or sniff(head, media_type)[0]
    mime, ext = sniff(head, media_type)
    safe_id = "".join(c if c.isalnum() or c in "-_" else "_" for c in message_id)
    return f"{media_type or 'media'}_{safe_id}{ext}", mime


def media_uri(chat_jid: str, message_id: str) -> str | None:
    """The resource URI of a message's media, or None when an id could not survive
    the round trip through the template."""
    if not chat_jid or not message_id or _URI_UNSAFE & set(chat_jid + message_id):
        return None
    return MEDIA_URI_TEMPLATE.format(chat_jid=chat_jid, message_id=message_id)


def media_file(message_id: str, chat_jid: str) -> MediaFile | None:
    """Download a message's media through the tenant's bridge and describe it.

    None when the active tenant's store has no such media message, or the bridge
    could not download it: WhatsApp expires media keys, so that is the ordinary
    outcome for old media. The store is read first, so a message id from another
    tenant never reaches the bridge. Raises OSError when the downloaded file cannot
    be read.
    """
    meta = get_media_meta(message_id, chat_jid)
    if meta is None or meta[0] not in _DOWNLOADABLE:
        return None
    path = whatsapp_actions.download_media(message_id, chat_jid)
    if not path:
        return None
    size = os.path.getsize(path)
    with open(path, "rb") as handle:
        head = handle.read(_SNIFF_BYTES)
    name, mime = describe(meta[0], meta[1], message_id, head)
    return MediaFile(path=path, name=name, mime_type=mime, size_bytes=size)
```

In `whatsapp-mcp-server/pyproject.toml`, add `"media_files",` to `[tool.setuptools] py-modules`, between `"main",` and `"mcp_config",`.

- [ ] **Step 7: Run the tests.** Run `wa_build.sh test`.

Expected: `go-exit=0`, all tests passed (115 plus this file's), ruff clean.

- [ ] **Step 8: Commit.**

```bash
cd /d/tmp/aura-whatsapp-mcp
git add whatsapp-mcp-server/media_files.py whatsapp-mcp-server/tests/test_media_files.py
git commit -F - -- whatsapp-mcp-server/media_files.py whatsapp-mcp-server/pyproject.toml whatsapp-mcp-server/whatsapp_query_messages.py whatsapp-mcp-server/whatsapp_actions.py whatsapp-mcp-server/tests/conftest.py whatsapp-mcp-server/tests/test_media_files.py <<'EOF'
feat(media): name and type a received file from its row and its bytes

The bridge's download returns a path and nothing else, and the path lies:
every image is saved as .jpg, PNGs included, and a document loses the name
its sender gave it. media_files reads the message row (media_type and
filename) from the caller's own store, names a document by its sender's
name (last segment only) and types everything else by its first bytes. A
message id the tenant's store does not have, or a row that is no file (a
reaction), never reaches the bridge.

The bridge download also gets a 70 s timeout, above the tenant gateway's
65 s; it had none.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
EOF
```

---

### Task 3: `download_media` links the file, and the link reads back

**Files:**
- Modify: `whatsapp-mcp-server/media_files.py` (append `download_result`, `read_bytes`)
- Modify: `whatsapp-mcp-server/main.py`:
  - the imports;
  - the module docstring;
  - `download_media`;
  - a new `read_media` resource.
- Modify: `whatsapp-mcp-server/tenant_context.py` (`SubjectTenantMiddleware`)
- Modify: `README.md` (`send_file`, `send_audio_message`, `download_media`), `AGENTS.md` ("Where to make changes")
- Test: `whatsapp-mcp-server/tests/test_download_media_tool.py`, `whatsapp-mcp-server/tests/test_tenancy.py`

**Interfaces:**
- Consumes:
  - `media_file`, `media_uri`, `MAX_MEDIA_FILE_BYTES`, `MEDIA_URI_TEMPLATE`, `OCTET_STREAM` (Task 2);
  - the fixtures `seed_media` and `bridge_download` (Task 2).
- Produces:
  - `media_files.download_result(message_id, chat_jid) -> CallToolResult`;
  - `media_files.read_bytes(message_id, chat_jid) -> bytes`;
  - the MCP tool `download_media -> CallToolResult`;
  - the resource template `whatsapp-media://{chat_jid}/{message_id}`.

- [ ] **Step 1: Write the failing tests.** Create `whatsapp-mcp-server/tests/test_download_media_tool.py`:

```python
"""download_media and its whatsapp-media:// resource, driven through a real MCP client.

`Client(main.mcp)` talks to the server in-process, so every request crosses the same
middleware a remote one does. The caller's OAuth subject is never the tenant the
suite binds by default: a read that skipped the tenant middleware would run as that
default tenant, find no store, and fail.
"""

import base64
import json

import pytest
from mcp import Client
from mcp.server.auth.provider import AccessToken
from mcp.shared.exceptions import MCPError
from mcp_types import ResourceLink, TextContent

import main
import media_files
import tenant_context
import whatsapp_actions

TENANT_A = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
TENANT_B = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
CHAT = "393331234567@s.whatsapp.net"
MSG = "3EB0C767D26A1D3B9A0C"
PNG = b"\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"


@pytest.fixture
def signed_in(monkeypatch):
    """Make every request arrive with `subject`'s bearer token."""

    def as_subject(subject: str) -> None:
        monkeypatch.setattr(
            tenant_context,
            "get_access_token",
            lambda: AccessToken(token="token", client_id="client", scopes=["mcp:tools"], subject=subject),
        )

    return as_subject


def _body(result) -> dict:
    return json.loads(next(block for block in result.content if isinstance(block, TextContent)).text)


def _links(result) -> list[ResourceLink]:
    return [block for block in result.content if isinstance(block, ResourceLink)]


@pytest.mark.asyncio
async def test_download_media_links_the_file_and_the_link_reads_back(signed_in, seed_media, bridge_download):
    signed_in(TENANT_A)
    seed_media(TENANT_A, MSG, CHAT, "image")
    bridge_download(PNG)

    async with Client(main.mcp) as client:
        result = await client.call_tool("download_media", {"message_id": MSG, "chat_jid": CHAT})
        [link] = _links(result)
        read = await client.read_resource(str(link.uri))

    body = _body(result)
    assert result.is_error is False
    assert body["success"] is True
    assert (body["name"], body["mime_type"], body["size_bytes"]) == (f"image_{MSG}.png", "image/png", len(PNG))
    assert body["file_path"].endswith("bridge-download.jpg")
    assert str(link.uri) == f"whatsapp-media://{CHAT}/{MSG}"
    assert (link.name, link.mime_type, link.size) == (f"image_{MSG}.png", "image/png", len(PNG))
    [contents] = read.contents
    assert base64.b64decode(contents.blob) == PNG


@pytest.mark.asyncio
async def test_another_tenant_reads_nothing_and_never_reaches_the_bridge(signed_in, seed_media, bridge_download):
    seed_media(TENANT_A, MSG, CHAT, "image")
    seed_media(TENANT_B, "B-OWN-MESSAGE", CHAT, "image")
    calls = bridge_download(PNG)
    signed_in(TENANT_B)

    async with Client(main.mcp) as client:
        with pytest.raises(MCPError, match="no downloadable media"):
            await client.read_resource(f"whatsapp-media://{CHAT}/{MSG}")

    assert calls == []


@pytest.mark.asyncio
async def test_a_failed_download_says_so_and_links_nothing(signed_in, seed_media, bridge_download):
    signed_in(TENANT_A)
    seed_media(TENANT_A, MSG, CHAT, "image")
    bridge_download(None)

    async with Client(main.mcp) as client:
        result = await client.call_tool("download_media", {"message_id": MSG, "chat_jid": CHAT})

    assert _body(result) == {"success": False, "message": "Failed to download media"}
    assert _links(result) == []


@pytest.mark.asyncio
async def test_media_over_the_cap_is_linked_with_its_size_but_not_served(
    signed_in, seed_media, bridge_download, monkeypatch
):
    """The link states the size, so a client can decline before it reads."""
    monkeypatch.setattr(media_files, "MAX_MEDIA_FILE_BYTES", 8)
    signed_in(TENANT_A)
    seed_media(TENANT_A, MSG, CHAT, "image")
    bridge_download(PNG)

    async with Client(main.mcp) as client:
        result = await client.call_tool("download_media", {"message_id": MSG, "chat_jid": CHAT})
        [link] = _links(result)
        with pytest.raises(MCPError, match="exceeds the 8-byte cap"):
            await client.read_resource(str(link.uri))

    assert link.size == len(PNG)


@pytest.mark.asyncio
async def test_a_message_id_a_uri_cannot_carry_gets_the_file_without_a_link(signed_in, seed_media, bridge_download):
    signed_in(TENANT_A)
    seed_media(TENANT_A, "ODD/ID", CHAT, "image")
    bridge_download(PNG)

    async with Client(main.mcp) as client:
        result = await client.call_tool("download_media", {"message_id": "ODD/ID", "chat_jid": CHAT})

    body = _body(result)
    assert body["success"] is True
    assert "no resource link" in body["message"]
    assert _links(result) == []


@pytest.mark.asyncio
async def test_a_download_whose_file_is_gone_is_reported_not_raised(signed_in, seed_media, monkeypatch, tmp_path):
    signed_in(TENANT_A)
    seed_media(TENANT_A, MSG, CHAT, "image")
    monkeypatch.setattr(whatsapp_actions, "download_media", lambda *_: str(tmp_path / "gone.jpg"))

    async with Client(main.mcp) as client:
        result = await client.call_tool("download_media", {"message_id": MSG, "chat_jid": CHAT})
        with pytest.raises(MCPError, match="media unreadable"):
            await client.read_resource(f"whatsapp-media://{CHAT}/{MSG}")

    body = _body(result)
    assert body["success"] is False
    assert body["message"].startswith("Media downloaded but unreadable")


@pytest.mark.asyncio
async def test_get_media_data_keeps_the_shape_the_view_reads(signed_in, bridge_download):
    """ui/_bridge.js `payloadOf` reads structuredContent when present, else the first
    text block's JSON; the view then needs `success` and a data:image/ URL."""
    signed_in(TENANT_A)
    bridge_download(PNG)

    async with Client(main.mcp) as client:
        result = await client.call_tool("get_media_data", {"message_id": MSG, "chat_jid": CHAT})

    payload = result.structured_content or _body(result)
    assert payload["success"] is True
    assert payload["data_url"].startswith("data:image/jpeg;base64,")
```

In `whatsapp-mcp-server/tests/test_tenancy.py`, add these two tests after `test_middleware_rejects_missing_subject_before_tool`:

```python
@pytest.mark.asyncio
async def test_middleware_binds_identity_for_resource_read(monkeypatch):
    """download_media links a whatsapp-media:// resource; reading it touches the same
    tenant store the tool did, so it needs the same binding."""
    middleware = SubjectTenantMiddleware()
    ctx = SimpleNamespace(method="resources/read", meta=None)
    monkeypatch.setattr(
        tenant_context,
        "get_access_token",
        lambda: AccessToken(token="token", client_id="client", scopes=["mcp:tools"], subject=TENANT_B),
    )

    async def call_next(_ctx):
        return {"identity": current_identity()}

    assert await middleware(ctx, call_next) == {"identity": TENANT_B}


@pytest.mark.asyncio
async def test_middleware_leaves_listing_unbound(monkeypatch):
    """Listing tools or templates reads no tenant store and needs no subject."""
    middleware = SubjectTenantMiddleware()
    ctx = SimpleNamespace(method="resources/templates/list", meta=None)
    monkeypatch.setattr(tenant_context, "get_access_token", lambda: None)

    async def call_next(_ctx):
        return "listed"

    assert await middleware(ctx, call_next) == "listed"
```

- [ ] **Step 2: Run the tests to verify they fail.** Run `wa_build.sh test`.

Expected FAILs:
- `test_download_media_links_the_file_and_the_link_reads_back`: `ValueError` unpacking `_links(result)`, because the tool returns a dict and no link.
- `test_another_tenant_reads_nothing_and_never_reaches_the_bridge`: `MCPError` with an unknown-resource message, not "no downloadable media".
- `test_media_over_the_cap_is_linked_with_its_size_but_not_served`: fails the same way as the first test.
- `test_middleware_binds_identity_for_resource_read`: the identity is the suite default, not `TENANT_B`.
- `test_a_message_id_a_uri_cannot_carry_gets_the_file_without_a_link` and `test_a_download_whose_file_is_gone_is_reported_not_raised` fail on the missing keys in today's dict.
- `test_a_failed_download_says_so_and_links_nothing` and `test_get_media_data_keeps_the_shape_the_view_reads` may already pass. They pin behavior that must not change.

- [ ] **Step 3: Implement the result and the read.** In `whatsapp-mcp-server/media_files.py`, first extend the imports:
  - add `import json` before `import mimetypes`;
  - add `from typing import Any` after `from pathlib import PurePosixPath`;
  - add a third-party block between the standard-library block and `import whatsapp_actions`:

```python
from mcp.server.mcpserver.exceptions import ResourceError, ResourceNotFoundError
from mcp_types import CallToolResult, ResourceLink, TextContent
```

Then append to the file:

```python


def download_result(message_id: str, chat_jid: str) -> CallToolResult:
    """download_media's result: the file described as JSON, plus a link to its bytes.

    `file_path` stays in the JSON because `send_file` forwards received media by it.
    """
    try:
        media = media_file(message_id, chat_jid)
    except OSError as err:
        return _json_result({"success": False, "message": f"Media downloaded but unreadable: {err}"})
    if media is None:
        return _json_result({"success": False, "message": "Failed to download media"})
    body = {
        "success": True,
        "message": "Media downloaded successfully",
        "name": media.name,
        "mime_type": media.mime_type,
        "size_bytes": media.size_bytes,
        "file_path": media.path,
    }
    uri = media_uri(chat_jid, message_id)
    if uri is None:
        body["message"] += "; no resource link, because this message id cannot be carried in a URI"
        return _json_result(body)
    link = ResourceLink(uri=uri, name=media.name, mime_type=media.mime_type, size=media.size_bytes)
    return CallToolResult(content=[TextContent(text=json.dumps(body, ensure_ascii=False)), link])


def read_bytes(message_id: str, chat_jid: str) -> bytes:
    """The media's bytes for `resources/read`.

    ResourceNotFoundError and ResourceError reach the client with their message;
    any other exception would reach it as a bare "Error reading resource".
    """
    try:
        media = media_file(message_id, chat_jid)
        if media is None:
            raise ResourceNotFoundError(f"no downloadable media for message {message_id} in {chat_jid}")
        if media.size_bytes > MAX_MEDIA_FILE_BYTES:
            raise ResourceError(f"{media.size_bytes} bytes exceeds the {MAX_MEDIA_FILE_BYTES}-byte cap")
        with open(media.path, "rb") as handle:
            return handle.read()
    except OSError as err:
        raise ResourceError(f"media unreadable: {err}") from err


def _json_result(body: dict[str, Any]) -> CallToolResult:
    return CallToolResult(content=[TextContent(text=json.dumps(body, ensure_ascii=False))])
```

- [ ] **Step 4: Wire the tool and the resource.** In `whatsapp-mcp-server/main.py`:

1. **Imports.** Add `from mcp_types import CallToolResult` directly after `from mcp.server.mcpserver import MCPServer`. Add `import media_files` as the first line of the local-module block, directly before `from apps_ui import CLIENT_URI, build_apps, openai_alias`. That is ruff's order: plain `import` lines come before `from` lines within a section.

2. **Module docstring.** After the sentence ending `Every host receives the same tool payload either way.`, add:

```
`download_media` also links a `whatsapp-media://` resource (`media_files`) that a
client reads the file's bytes back through, on the same authenticated session.
```

3. **The tool.** Replace the whole `download_media` function, from `@mcp.tool()` to its last `return`, with:

```python
@mcp.tool()
def download_media(message_id: str, chat_jid: str) -> CallToolResult:
    """Download the media of a received WhatsApp message.

    Returns JSON with the file's name, MIME type, size and local file path (which
    send_file accepts to forward it), plus a resource link a client reads the bytes
    back through.

    Args:
        message_id: The ID of the message containing the media
        chat_jid: The JID of the chat containing the message
    """
    return media_files.download_result(message_id, chat_jid)


@mcp.resource(media_files.MEDIA_URI_TEMPLATE, name="whatsapp-media", mime_type=media_files.OCTET_STREAM)
def read_media(chat_jid: str, message_id: str) -> bytes:
    """The bytes of a message's media: the file download_media linked to, up to 25 MiB."""
    return media_files.read_bytes(message_id, chat_jid)
```

In `whatsapp-mcp-server/tenant_context.py`, replace the `SubjectTenantMiddleware` class header and its first check:

```python
class SubjectTenantMiddleware:
    """Bind each tool call to the authenticated OAuth subject."""

    async def __call__(self, ctx: ServerRequestContext[Any, Any], call_next: CallNext):
        if ctx.method != "tools/call":
            return await call_next(ctx)
```

with:

```python
# Every method that reaches a tenant's store: a tool call, and the read of a
# resource a tool linked (download_media's whatsapp-media://).
_TENANT_METHODS = frozenset({"tools/call", "resources/read"})


class SubjectTenantMiddleware:
    """Bind each tool call and resource read to the authenticated OAuth subject."""

    async def __call__(self, ctx: ServerRequestContext[Any, Any], call_next: CallNext):
        if ctx.method not in _TENANT_METHODS:
            return await call_next(ctx)
```

- [ ] **Step 5: Run the tests.** Run `wa_build.sh test`. Expected: `go-exit=0`, every test passed, ruff clean.

- [ ] **Step 6: Correct the docs.** In `README.md`:

1. **`send_file`.** Replace the parameter lines

```
- `file_path` (required): Path to the file
- `caption` (optional): Caption for the media
```

   with `- \`media_path\` (required): Absolute path to the file`. The tool has always taken `media_path` and has no caption.

2. **`send_audio_message`.** Replace `- \`file_path\` (required): Path to audio file` with `- \`media_path\` (required): Absolute path to the audio file`.

3. **`download_media`.** Replace the whole section, from `#### \`download_media\`` up to, but not including, `### Chat Operations`, with:

````markdown
#### `download_media`

Download the media of a received message.

**Parameters:**

- `message_id` (required): ID of the message with media
- `chat_jid` (required): JID of the chat containing the message

**Returns** two content blocks:

- JSON text with `success`, `message`, `name`, `mime_type`, `size_bytes` and
  `file_path`. `file_path` is inside the server's container; pass it to
  `send_file` to forward the media.
- A `resource_link` to `whatsapp-media://{chat_jid}/{message_id}`. A client that
  needs the bytes reads it with `resources/read` on the same authenticated
  session, so the bytes never pass through the model. Reads above 25 MiB are
  refused; the link states the size first.

A document keeps the name its sender gave it. Other media are named
`<type>_<message_id><ext>` and typed by their first bytes, because the bridge
saves every image as `.jpg`.

````

In `AGENTS.md`, in the "Where to make changes" table, add this row after `| Change DB queries / data conversion | ... |`:

```
| Change how received media is named, typed or served | `whatsapp-mcp-server/media_files.py` |
```

- [ ] **Step 7: Commit.**

```bash
cd /d/tmp/aura-whatsapp-mcp
git add whatsapp-mcp-server/tests/test_download_media_tool.py
git commit -F - -- whatsapp-mcp-server/media_files.py whatsapp-mcp-server/main.py whatsapp-mcp-server/tenant_context.py whatsapp-mcp-server/tests/test_download_media_tool.py whatsapp-mcp-server/tests/test_tenancy.py README.md AGENTS.md <<'EOF'
feat(media): download_media links the file as whatsapp-media://

download_media returned a path inside this container, which no client can
open: the bytes had no way out except get_media_data's base64, paid for in
the model's context. It now returns a CallToolResult: the JSON (name,
mime_type, size_bytes, and the file_path send_file still forwards by) plus
a resource_link to whatsapp-media://{chat_jid}/{message_id}, which a client
reads with resources/read on its own session. Reads are refused above
25 MiB with the size in the message.

The URI names no path and no tenant; the tenant comes from the caller's
token. SubjectTenantMiddleware bound it for tools/call only, so a resource
read ran under whatever tenant was bound before; it now binds resources/read
too. The tests drive a real client through that middleware as a tenant the
suite does not bind by default.

get_media_data, the view's contract, is unchanged and pinned through the
client. The README's send_file and send_audio_message documented a
file_path/caption pair the tools never took.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
EOF
```

---

### Task 4: Full gates, then publish

- [ ] **Step 1: Run every gate on the final tree.** Run `wa_build.sh test`, then `wa_build.sh audit`.

Expected: every result exactly as in Task 1 Steps 8 and 10, with the new tests included.

- [ ] **Step 2: Read the whole change once.** Run `git log --oneline origin/main..main` and expect three commits. Then run `git diff origin/main --stat`. Nothing outside the files named in Tasks 1–3 may appear.

- [ ] **Step 3: Push. ASK THE OPERATOR FIRST.** Pushing `main` runs CI and `publish-image.yml`, which moves `ghcr.io/chetto1983/whatsapp-mcp:latest`, and every appliance's updater refreshes to that tag. With the go:

```bash
cd /d/tmp/aura-whatsapp-mcp
git push origin main
gh run list --repo chetto1983/whatsapp-mcp --limit 5
gh run watch --repo chetto1983/whatsapp-mcp $(gh run list --repo chetto1983/whatsapp-mcp --workflow publish-image.yml --limit 1 --json databaseId -q '.[0].databaseId')
```

Expected: `CI` green, with all blocking jobs listed in `AGENTS.md`, `Go Build` now including its `Test` step. `Publish Image` green, which is the first build of the Dockerfile on `golang:1.27.1-bookworm` and uv 0.12.18. `Security` may report on its informational jobs; read them.
