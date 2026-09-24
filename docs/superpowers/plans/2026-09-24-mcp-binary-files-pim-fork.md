# MCP binary files: PIM fork implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `get_email_attachment` returns a `resource_link` to `attachment://<id>`. Any MCP client, Aura included, reads the attachment's bytes back through `resources/read` on its own session.

**Architecture:**

- **Resource.** A new `EmailAttachmentResource` serves `attachment://{attachmentId}` from the existing tenant-scoped `InMemoryAttachmentStore`. The factory binds the tenant from the request principal, which is the same principal the `calendar` tool binds from.
- **Tool.** The curated `calendar` tool returns a `CallToolResult`. `get_email_attachment` always stashes, and adds a `ResourceLinkBlock` next to the stash JSON. Every other action returns its JSON as one text block, which is what clients get today.
- **Validation.** The code in Tasks 2 and 3 was spiked on 2026-09-24 in a WSL copy of this branch: 588/588 tests passed, including the in-process MCP round trips below.

**Tech Stack:** .NET 10, ModelContextProtocol C# SDK 2.2.0, MSTest 4.3, Rocks 10.3, MimeKit (through MailKit 4.17).

**Spec:** `D:\Aura\docs\superpowers\specs\2026-09-24-mcp-binary-files-design.md`, section "PIM fork (`aura-pim-mcp`)".

## Global Constraints

**Repository and commits**
- Repository: `D:\tmp\aura-pim-mcp`. Work on branch `aura/upstream-1.8.3`, which already holds the 1.8.3 merge plus six fix commits and is not pushed. This plan merges it into `main` in Task 4.
- Commit with explicit paths (`git commit -- <paths>`). End every message with `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`. Never use `--no-verify`.
- Pushing `main` publishes `ghcr.io/chetto1983/aura-pim-mcp:{latest,sidecar,<sha>}` through `.github/workflows/aura-publish-image.yml`. It needs the operator's explicit go.

**Build and test**
- Build and test in WSL through the scratchpad script `pim_build.sh`. It rsyncs `/mnt/d/tmp/aura-pim-mcp/` to `~/pim-build` and runs `dotnet build|test calendar-mcp.slnx -c Release`.
- Never run a Windows `.exe`.
- The Windows working tree uses CRLF line endings. Edit with the Edit tool, never `sed`, because `$` does not match before `\r`.

**Values from the spec**
- The per-attachment store cap is **25 MiB** (`25 * 1024 * 1024`). The 100 MiB total and the 15-minute TTL are unchanged.
- The resource URI template is `attachment://{attachmentId}`, and the resource MIME type is `application/octet-stream`.
- MIME rule: use the stored content type unless it is empty or `application/octet-stream`; otherwise use `MimeKit.MimeTypes.GetMimeType(name)`.
- Unknown or expired ID error, verbatim: `attachment expired or unknown; call get_email_attachment again`.

**Upstream code**
- Upstream's `GetEmailAttachmentTool` class is NOT modified. The fork calls it with `mode: "stash"` and removes `mode` from the curated schema only.

## Review Focus

1. **Stdio client.** The stdio server's incoming message filter sets the local principal, and the resource factory must receive it. It is the same `request.User` path as HTTP. Pinned by the harness in Task 2, which sets the principal the way stdio does.
2. **Reading before forwarding.** A client reads the attachment and then forwards it with `send_email`. The read uses `TryRead`, which must not consume the stash. Pinned in Task 2 (`ResourcesRead_DoesNotConsumeTheStash`).
3. **Upstream drops a field.** If upstream renames or drops `attachmentId`, `name` or `size` in the stash JSON, the tool must fail loudly instead of emitting a dead link. Pinned in Task 3 (`WithAttachmentLink_FailsLoudlyWhenUpstreamShapeChanges`).
4. **Generic content type.** A Gmail attachment arrives with `contentType` `application/octet-stream` and a `.pdf` name. The link and the blob must both say `application/pdf`. Pinned in Task 2 (the `MimeTypeFor` table) and in Task 3 (the round trip uses `ContentType = null`).
5. **Upload cap message.** An upload between 10 and 25 MiB must be accepted, and the 413 message must state the real cap. The hard-coded "10 MB" goes in Task 1, and the store test pins the new default.

---

### Task 1: Store cap 25 MiB, one definition

**Files:**
- Modify: `src/CalendarMcp.Core/Services/IAttachmentStore.cs` (`AttachmentStoreOptions.MaxBytesPerAttachment`)
- Modify: `src/CalendarMcp.HttpServer/Endpoints/AttachmentEndpoints.cs` (`UploadAsync`: the check and the 413 text)
- Test: `src/CalendarMcp.Tests/Services/InMemoryAttachmentStoreTests.cs`

**Interfaces:**
- Produces: `AttachmentStoreOptions.MaxBytesPerAttachment` defaults to `25 * 1024 * 1024`. `UploadAsync` reads the cap from `IOptions<AttachmentStoreOptions>`.

- [ ] **Step 1: Write the failing test.** Add it to `InMemoryAttachmentStoreTests`, just before the private `FakeTimeProvider` class:

```csharp
    [TestMethod]
    public void DefaultCap_AdmitsA25MiBAttachmentAndRefusesOneByteMore()
    {
        const int cap = 25 * 1024 * 1024;
        var store = CreateStore();

        Assert.IsNotNull(store.Put("max.bin", null, new byte[cap]));
        Assert.IsNull(store.Put("over.bin", null, new byte[cap + 1]));
    }
```

- [ ] **Step 2: Run it to verify it fails.**

Run: `MSYS_NO_PATHCONV=1 wsl bash <scratchpad>/pim_build.sh test`

Expected: FAIL in `DefaultCap_AdmitsA25MiBAttachmentAndRefusesOneByteMore`. The first `Put` returns null, because the cap is still 10 MiB.

- [ ] **Step 3: Implement.** In `IAttachmentStore.cs`:

```csharp
public sealed class AttachmentStoreOptions
{
    // Gmail's attachment limit, and the cap Aura's MCP bridge materializes a file under.
    public int MaxBytesPerAttachment { get; set; } = 25 * 1024 * 1024;
    public long MaxTotalBytes { get; set; } = 100L * 1024 * 1024;
    public TimeSpan Ttl { get; set; } = TimeSpan.FromMinutes(15);
}
```

In `AttachmentEndpoints.cs`, first add `IOptions<AttachmentStoreOptions> options` as a parameter of `UploadAsync`, after `IAttachmentStore store`. Minimal-API binding resolves it from DI. Add `using Microsoft.Extensions.Options;` if it is missing.

Then replace the hard-coded block:

```csharp
            if (bytes.Length > 10 * 1024 * 1024)
            {
                return Results.Problem(
                    title: "Attachment too large",
                    detail: "Each upload must be 10 MB or less.",
                    statusCode: StatusCodes.Status413PayloadTooLarge);
            }
```

with:

```csharp
            var cap = options.Value.MaxBytesPerAttachment;
            if (bytes.Length > cap)
            {
                return Results.Problem(
                    title: "Attachment too large",
                    detail: $"Each upload must be {cap / (1024 * 1024)} MiB or less.",
                    statusCode: StatusCodes.Status413PayloadTooLarge);
            }
```

- [ ] **Step 4: Run the tests.** Command as in Step 2. Expected: `Passed!`, 0 failed.

- [ ] **Step 5: Commit.**

```bash
cd /d/tmp/aura-pim-mcp
git commit -F - -- src/CalendarMcp.Core/Services/IAttachmentStore.cs src/CalendarMcp.HttpServer/Endpoints/AttachmentEndpoints.cs src/CalendarMcp.Tests/Services/InMemoryAttachmentStoreTests.cs <<'EOF'
feat(attachments): raise the per-attachment cap to 25 MiB, stated once

Gmail sends attachments up to 25 MB, and Aura's MCP bridge materializes a file
up to 25 MiB; a 10 MiB stash refused the rest before anyone could read them.
The upload endpoint repeated the old cap as a literal in its check and its
413 text; both now read AttachmentStoreOptions.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
EOF
```

---

### Task 2: The `attachment://{attachmentId}` resource

**Files:**
- Create: `src/CalendarMcp.Core/Tools/EmailAttachmentResource.cs`
- Create: `src/CalendarMcp.Tests/Helpers/InProcessMcpSession.cs`
- Create: `src/CalendarMcp.Tests/Tools/EmailAttachmentResourceTests.cs`
- Modify: `src/CalendarMcp.HttpServer/Program.cs`, after `.WithCalendarView()`
- Modify: `src/CalendarMcp.StdioServer/Program.cs`, after `.WithCalendarView()`

**Interfaces:**
- Produces:
  - `EmailAttachmentResource.UriTemplate` (const `"attachment://{attachmentId}"`)
  - `EmailAttachmentResource.UriFor(string attachmentId) : string`
  - `EmailAttachmentResource.MimeTypeFor(string name, string? contentType) : string` (internal static)
  - `IMcpServerBuilder.WithEmailAttachmentResource()`
  - Test helper `InProcessMcpSession.StartAsync(ITenantContext, IAttachmentStore, string tenant, Action<IServiceCollection>? configure = null)`. Its `Client` is an `McpClient`.

- [ ] **Step 1: Write the test harness.** Create `src/CalendarMcp.Tests/Helpers/InProcessMcpSession.cs`:

```csharp
using System.IO.Pipelines;
using CalendarMcp.Core.Services;
using CalendarMcp.Core.Tenancy;
using CalendarMcp.Core.Tools;
using Microsoft.Extensions.DependencyInjection;
using Microsoft.Extensions.Logging.Abstractions;
using Microsoft.Extensions.Options;
using ModelContextProtocol.Client;
using ModelContextProtocol.Protocol;
using ModelContextProtocol.Server;

namespace CalendarMcp.Tests.Helpers;

/// <summary>
/// A real MCP client and server over in-memory pipes, registered the way both hosts register
/// the curated tool and the attachment resource. The request principal is set by an incoming
/// message filter -- exactly how the stdio server supplies it -- so a test exercises the same
/// tenant path a host does instead of calling the resource method directly.
/// </summary>
internal sealed class InProcessMcpSession : IAsyncDisposable
{
    private readonly ServiceProvider _services;
    private readonly McpServer _server;
    private readonly CancellationTokenSource _stop;
    private readonly Task _running;

    private InProcessMcpSession(ServiceProvider services, McpServer server, CancellationTokenSource stop, Task running, McpClient client)
    {
        _services = services;
        _server = server;
        _stop = stop;
        _running = running;
        Client = client;
    }

    public McpClient Client { get; }

    public static async Task<InProcessMcpSession> StartAsync(
        ITenantContext tenantContext, IAttachmentStore store, string tenant, Action<IServiceCollection>? configure = null)
    {
        var principal = TenantIdentity.LocalPrincipal(tenant);
        var services = new ServiceCollection();
        services.AddLogging();
        services.AddSingleton(tenantContext);
        services.AddSingleton(store);
        configure?.Invoke(services);
        services.AddMcpServer()
            .WithMessageFilters(filters => filters.AddIncomingFilter(next => (context, cancellationToken) =>
            {
                context.User = principal;
                return next(context, cancellationToken);
            }))
            .WithCalendarActionTool()
            .WithEmailAttachmentResource();
        var provider = services.BuildServiceProvider();

        var clientToServer = new Pipe();
        var serverToClient = new Pipe();
        var server = McpServer.Create(
            new StreamServerTransport(clientToServer.Reader.AsStream(), serverToClient.Writer.AsStream(), "test", NullLoggerFactory.Instance),
            provider.GetRequiredService<IOptions<McpServerOptions>>().Value,
            NullLoggerFactory.Instance,
            provider);
        var stop = new CancellationTokenSource();
        var running = server.RunAsync(stop.Token);
        var client = await McpClient.CreateAsync(
            new StreamClientTransport(clientToServer.Writer.AsStream(), serverToClient.Reader.AsStream(), NullLoggerFactory.Instance));
        return new InProcessMcpSession(provider, server, stop, running, client);
    }

    public async ValueTask DisposeAsync()
    {
        await Client.DisposeAsync();
        await _stop.CancelAsync();
        try
        {
            await _running;
        }
        catch (OperationCanceledException)
        {
        }
        await _server.DisposeAsync();
        await _services.DisposeAsync();
        _stop.Dispose();
    }
}
```

- [ ] **Step 2: Write the failing tests.** Create `src/CalendarMcp.Tests/Tools/EmailAttachmentResourceTests.cs`:

```csharp
using CalendarMcp.Core.Services;
using CalendarMcp.Core.Tenancy;
using CalendarMcp.Core.Tools;
using CalendarMcp.Tests.Helpers;
using Microsoft.Extensions.Logging.Abstractions;
using Microsoft.Extensions.Options;
using ModelContextProtocol;
using ModelContextProtocol.Protocol;

namespace CalendarMcp.Tests.Tools;

[TestClass]
public sealed class EmailAttachmentResourceTests
{
    [TestMethod]
    public async Task ResourcesRead_ReturnsTheStashedBytesToTheTenantThatStashedThem()
    {
        var tenantContext = new TenantContext();
        var store = NewStore(tenantContext);
        var stored = Stash(store, tenantContext, TestData.TenantA, "invoice.pdf", null, "%PDF-1.7"u8.ToArray());

        await using var session = await InProcessMcpSession.StartAsync(tenantContext, store, TestData.TenantA);
        var result = await session.Client.ReadResourceAsync(EmailAttachmentResource.UriFor(stored.Id));

        var blob = (BlobResourceContents)result.Contents.Single();
        Assert.AreEqual("application/pdf", blob.MimeType);
        CollectionAssert.AreEqual("%PDF-1.7"u8.ToArray(), blob.DecodedData.ToArray());
    }

    [TestMethod]
    public async Task ResourcesRead_RefusesAnotherTenantsAttachment()
    {
        var tenantContext = new TenantContext();
        var store = NewStore(tenantContext);
        var stored = Stash(store, tenantContext, TestData.TenantA, "invoice.pdf", "application/pdf", [1, 2, 3]);

        await using var session = await InProcessMcpSession.StartAsync(tenantContext, store, TestData.TenantB);
        var error = await Assert.ThrowsAsync<McpException>(
            () => session.Client.ReadResourceAsync(EmailAttachmentResource.UriFor(stored.Id)).AsTask());

        StringAssert.Contains(error.Message, "attachment expired or unknown; call get_email_attachment again");
    }

    [TestMethod]
    public async Task ResourcesRead_RefusesAnExpiredAttachment()
    {
        var tenantContext = new TenantContext();
        var store = NewStore(tenantContext, new AttachmentStoreOptions { Ttl = TimeSpan.Zero });
        var stored = Stash(store, tenantContext, TestData.TenantA, "invoice.pdf", "application/pdf", [1, 2, 3]);

        await using var session = await InProcessMcpSession.StartAsync(tenantContext, store, TestData.TenantA);
        var error = await Assert.ThrowsAsync<McpException>(
            () => session.Client.ReadResourceAsync(EmailAttachmentResource.UriFor(stored.Id)).AsTask());

        StringAssert.Contains(error.Message, "expired or unknown");
    }

    [TestMethod]
    public async Task ResourcesRead_DoesNotConsumeTheStash()
    {
        var tenantContext = new TenantContext();
        var store = NewStore(tenantContext);
        var stored = Stash(store, tenantContext, TestData.TenantA, "invoice.pdf", "application/pdf", [1, 2, 3]);

        await using (var session = await InProcessMcpSession.StartAsync(tenantContext, store, TestData.TenantA))
            await session.Client.ReadResourceAsync(EmailAttachmentResource.UriFor(stored.Id));

        using (tenantContext.Bind(TestData.TenantA))
            Assert.IsNotNull(store.TryConsume(stored.Id), "send_email must still find the stash after a read.");
    }

    [TestMethod]
    [DataRow("report.pdf", null, "application/pdf")]
    [DataRow("report.pdf", "application/octet-stream", "application/pdf")]
    [DataRow("report.pdf", "", "application/pdf")]
    [DataRow("photo.bin", "image/png", "image/png")]
    [DataRow("no-extension", null, "application/octet-stream")]
    public void MimeTypeFor_PrefersASpecificContentTypeThenTheName(string name, string? contentType, string expected)
    {
        Assert.AreEqual(expected, EmailAttachmentResource.MimeTypeFor(name, contentType));
    }

    private static InMemoryAttachmentStore NewStore(ITenantContext tenantContext, AttachmentStoreOptions? options = null) =>
        new(Options.Create(options ?? new AttachmentStoreOptions()), NullLogger<InMemoryAttachmentStore>.Instance, tenantContext);

    private static StoredAttachment Stash(
        IAttachmentStore store, ITenantContext tenantContext, string tenant, string name, string? contentType, byte[] bytes)
    {
        using (tenantContext.Bind(tenant))
            return store.Put(name, contentType, bytes)!;
    }
}
```

- [ ] **Step 3: Run the tests to verify they fail.** Command as in Task 1 Step 2.

Expected: build FAIL with `CS0103: The name 'EmailAttachmentResource' does not exist` (and `'WithEmailAttachmentResource'`).

- [ ] **Step 4: Implement the resource.** Create `src/CalendarMcp.Core/Tools/EmailAttachmentResource.cs`:

```csharp
using System.Security.Claims;
using CalendarMcp.Core.Services;
using CalendarMcp.Core.Tenancy;
using Microsoft.Extensions.DependencyInjection;
using ModelContextProtocol;
using ModelContextProtocol.Protocol;
using ModelContextProtocol.Server;

namespace CalendarMcp.Core.Tools;

/// <summary>
/// <c>attachment://{attachmentId}</c>: the bytes get_email_attachment stashed, read back by the
/// client its result linked them to. The tenant comes from the request principal -- the bearer's
/// <c>sub</c> over HTTP, the local tenant the stdio filter sets -- so an id minted for one tenant
/// reads as unknown to every other, exactly as it already does for send_email.
/// </summary>
/// <remarks>
/// Reads through <see cref="IAttachmentStore.TryRead"/>, which does not consume: a client that
/// opens the file and then forwards it with send_email must still find the stash.
/// </remarks>
public sealed class EmailAttachmentResource(IAttachmentStore store, ITenantContext tenantContext, ClaimsPrincipal? user)
{
    public const string UriTemplate = "attachment://{attachmentId}";

    public static string UriFor(string attachmentId) => "attachment://" + attachmentId;

    public BlobResourceContents Read(string attachmentId)
    {
        IDisposable scope;
        try
        {
            scope = tenantContext.Bind(TenantIdentity.FromPrincipal(user));
        }
        catch (ArgumentException ex)
        {
            throw new McpException(ex.Message);
        }
        using (scope)
        {
            var stored = store.TryRead(attachmentId)
                ?? throw new McpException("attachment expired or unknown; call get_email_attachment again");
            return BlobResourceContents.FromBytes(stored.Bytes, UriFor(stored.Id), MimeTypeFor(stored.Name, stored.ContentType));
        }
    }

    /// <summary>
    /// Providers often label an attachment <c>application/octet-stream</c>; the file name then
    /// knows more than the header, and a client choosing how to open the file needs the better one.
    /// </summary>
    internal static string MimeTypeFor(string name, string? contentType) =>
        string.IsNullOrWhiteSpace(contentType) || contentType == "application/octet-stream"
            ? MimeKit.MimeTypes.GetMimeType(name)
            : contentType;
}

/// <summary>
/// Registers <see cref="EmailAttachmentResource"/> through the factory overload of
/// <c>McpServerResource.Create</c>: it hands over the <c>RequestContext</c>, and with it the
/// request principal the tenant is bound from. Neither HTTP nor stdio exposes that principal
/// any other way to a resource method.
/// </summary>
public static class EmailAttachmentResourceServiceExtensions
{
    public static IMcpServerBuilder WithEmailAttachmentResource(this IMcpServerBuilder builder)
    {
        var method = typeof(EmailAttachmentResource).GetMethod(nameof(EmailAttachmentResource.Read))
            ?? throw new InvalidOperationException("EmailAttachmentResource.Read method not found.");
        builder.Services.AddSingleton<McpServerResource>(services => McpServerResource.Create(
            method,
            request =>
            {
                var provider = request.Services ?? services;
                return new EmailAttachmentResource(
                    provider.GetRequiredService<IAttachmentStore>(),
                    provider.GetRequiredService<ITenantContext>(),
                    request.User);
            },
            new McpServerResourceCreateOptions
            {
                UriTemplate = EmailAttachmentResource.UriTemplate,
                Name = "email-attachment",
                Title = "Email attachment",
                Description = "The bytes of an attachment get_email_attachment fetched; readable until the attachment expires.",
                MimeType = "application/octet-stream",
                Services = services,
            }));
        return builder;
    }
}
```

Register the resource on both servers. In `src/CalendarMcp.HttpServer/Program.cs`, replace:

```csharp
            .WithCalendarView()
```

with:

```csharp
            .WithCalendarView()
            // attachment://{id}: the file behind get_email_attachment's resource_link.
            .WithEmailAttachmentResource()
```

In `src/CalendarMcp.StdioServer/Program.cs`, make the same replacement on its `.WithCalendarView()` line, keeping that file's indentation.

- [ ] **Step 5: Run the tests.** Command as in Task 1 Step 2. Expected: `Passed!`, 0 failed.

- [ ] **Step 6: Commit.**

```bash
cd /d/tmp/aura-pim-mcp
git add src/CalendarMcp.Core/Tools/EmailAttachmentResource.cs src/CalendarMcp.Tests/Helpers/InProcessMcpSession.cs src/CalendarMcp.Tests/Tools/EmailAttachmentResourceTests.cs
git commit -F - -- src/CalendarMcp.Core/Tools/EmailAttachmentResource.cs src/CalendarMcp.Tests/Helpers/InProcessMcpSession.cs src/CalendarMcp.Tests/Tools/EmailAttachmentResourceTests.cs src/CalendarMcp.HttpServer/Program.cs src/CalendarMcp.StdioServer/Program.cs <<'EOF'
feat(attachments): serve a stashed attachment as attachment://{id}

The stash could reach a client only as an id for send_email or through
/attachments, which a sandboxed agent cannot reach and holds no bearer for.
A resource template makes the bytes readable over the MCP session the client
already has. It binds the tenant from the request principal (the bearer's sub,
or the local tenant the stdio filter sets), reads without consuming so a
forward still works afterwards, and names a better MIME type than
application/octet-stream when the file name knows one.

The tests drive a real client and server over in-memory pipes with the
principal set by a message filter, the same path stdio uses.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
EOF
```

---

### Task 3: `get_email_attachment` returns the link

**Files:**
- Create: `src/CalendarMcp.Core/Tools/CalendarActionTool.Attachments.cs`
- Modify: `src/CalendarMcp.Core/Tools/CalendarActionTool.cs`:
  - the tool description line for `get_email_attachment`;
  - the `mode` parameter (delete it);
  - `Calendar`'s return type and return statement;
  - the dispatch arm.
- Modify: `src/CalendarMcp.Core/Tools/CalendarActionTool.Delegated.cs` (delete `GetEmailAttachmentAction`)
- Modify: `src/CalendarMcp.Core/Tools/CalendarActionArguments.cs` (delete `Mode`)
- Modify: `src/CalendarMcp.Core/Skills/attachments.md`, the "Inbound" section, which the model reads through `get_guide`
- Test: `src/CalendarMcp.Tests/Tools/CalendarActionToolAttachmentTests.cs`

**Interfaces:**
- Consumes:
  - `EmailAttachmentResource.UriFor`, `EmailAttachmentResource.MimeTypeFor` and `InProcessMcpSession` (Task 2);
  - the private `ParseObject` and `RequireString` from `CalendarActionTool.Calendar.cs`.
- Produces:
  - `Calendar(...)` now returns `Task<CallToolResult>`;
  - `CalendarActionTool.WithAttachmentLink(string stashJson) : CallToolResult` (internal static);
  - `CalendarActionTool.TextResult(string text) : CallToolResult` (internal static).

- [ ] **Step 1: Write the failing tests.** Create `src/CalendarMcp.Tests/Tools/CalendarActionToolAttachmentTests.cs`:

```csharp
using CalendarMcp.Core.Models;
using CalendarMcp.Core.Services;
using CalendarMcp.Core.Tenancy;
using CalendarMcp.Core.Tools;
using CalendarMcp.Tests.Helpers;
using Microsoft.Extensions.DependencyInjection;
using Microsoft.Extensions.Logging.Abstractions;
using Microsoft.Extensions.Options;
using ModelContextProtocol.Protocol;
using Rocks;

namespace CalendarMcp.Tests.Tools;

[TestClass]
public sealed class CalendarActionToolAttachmentTests
{
    [TestMethod]
    public async Task GetEmailAttachment_ReturnsALinkTheResourceServes()
    {
        var tenantContext = new TenantContext();
        var store = new InMemoryAttachmentStore(
            Options.Create(new AttachmentStoreOptions()), NullLogger<InMemoryAttachmentStore>.Instance, tenantContext);
        var registry = new IAccountRegistryCreateExpectations();
        registry.Setups.GetAccountAsync("acc-1")
            .ReturnValue(Task.FromResult<AccountInfo?>(TestData.CreateAccount(id: "acc-1", provider: "microsoft365")));
        var provider = new IProviderServiceCreateExpectations();
        provider.Setups.GetEmailAttachmentContentAsync("acc-1", "email-1", "part-0", Arg.Any<CancellationToken>())
            .ReturnValue(Task.FromResult<EmailAttachmentContent?>(
                new EmailAttachmentContent { Name = "report.pdf", ContentType = null, Bytes = "%PDF-1.7"u8.ToArray() }));
        var factory = new IProviderServiceFactoryCreateExpectations();
        factory.Setups.GetProvider("microsoft365").ReturnValue(provider.Instance());

        await using var session = await InProcessMcpSession.StartAsync(tenantContext, store, TestData.TenantA, services =>
        {
            services.AddSingleton(registry.Instance());
            services.AddSingleton(factory.Instance());
        });
        var result = await session.Client.CallToolAsync("calendar", new Dictionary<string, object?>
        {
            ["action"] = "get_email_attachment",
            ["accountId"] = "acc-1",
            ["emailId"] = "email-1",
            ["attachmentId"] = "part-0",
        });

        Assert.AreNotEqual(true, result.IsError, string.Join(" | ", result.Content.OfType<TextContentBlock>().Select(t => t.Text)));
        StringAssert.Contains(result.Content.OfType<TextContentBlock>().Single().Text, "\"attachmentId\"");
        var link = result.Content.OfType<ResourceLinkBlock>().Single();
        Assert.AreEqual("report.pdf", link.Name);
        Assert.AreEqual("application/pdf", link.MimeType);
        Assert.AreEqual(8L, link.Size);
        var read = await session.Client.ReadResourceAsync(link.Uri);
        CollectionAssert.AreEqual("%PDF-1.7"u8.ToArray(), ((BlobResourceContents)read.Contents.Single()).DecodedData.ToArray());
    }

    [TestMethod]
    public void WithAttachmentLink_KeepsTheStashJsonAsText()
    {
        const string stash = """{"attachmentId":"abc","name":"a.png","contentType":"image/png","size":3,"expiresAt":"2026-09-24T10:00:00Z"}""";

        var result = CalendarActionTool.WithAttachmentLink(stash);

        Assert.AreEqual(stash, ((TextContentBlock)result.Content[0]).Text);
        var link = (ResourceLinkBlock)result.Content[1];
        Assert.AreEqual("attachment://abc", link.Uri);
        Assert.AreEqual("image/png", link.MimeType);
    }

    [TestMethod]
    [DataRow("""{"name":"a.png","size":3}""")]
    [DataRow("""{"attachmentId":"abc","size":3}""")]
    [DataRow("""{"attachmentId":"abc","name":"a.png"}""")]
    [DataRow("""{"attachmentId":42,"name":"a.png","size":3}""")]
    public void WithAttachmentLink_FailsLoudlyWhenUpstreamShapeChanges(string stash)
    {
        Assert.ThrowsExactly<InvalidOperationException>(() => CalendarActionTool.WithAttachmentLink(stash));
    }
}
```

- [ ] **Step 2: Run the tests to verify they fail.** Command as in Task 1 Step 2.

Expected: build FAIL with `CS0117: 'CalendarActionTool' does not contain a definition for 'WithAttachmentLink'`.

- [ ] **Step 3: Implement the link builder.** Create `src/CalendarMcp.Core/Tools/CalendarActionTool.Attachments.cs`:

```csharp
using System.Text.Json.Nodes;
using ModelContextProtocol.Protocol;

namespace CalendarMcp.Core.Tools;

/// <summary>
/// get_email_attachment always stashes, and its result links the stash as an
/// <c>attachment://</c> resource (<see cref="EmailAttachmentResource"/>): a client that wants
/// the bytes reads them back on its own session, and one that does not pays nothing for them.
/// The <c>attachmentId</c> in the text stays what <c>send_email</c> takes. Upstream's inline
/// mode is not offered: base64 in the result is paid for by the model's context either way.
/// </summary>
public sealed partial class CalendarActionTool
{
    private Task<string> GetEmailAttachmentAction(string? accountId, string? emailId, string? attachmentId) =>
        Impl<GetEmailAttachmentTool>().GetEmailAttachment(accountId!, emailId!, attachmentId!, "stash");

    /// <summary>
    /// The stash JSON as text plus a resource link to it. Fails loudly if upstream renames a
    /// field the link is built from: a link without its id would point at nothing.
    /// </summary>
    internal static CallToolResult WithAttachmentLink(string stashJson)
    {
        var stash = ParseObject(stashJson);
        var id = RequireString(stash, "attachmentId", "get_email_attachment");
        var name = RequireString(stash, "name", "get_email_attachment");
        var size = stash["size"] is JsonValue sizeValue && sizeValue.TryGetValue<long>(out var bytes)
            ? bytes
            : throw new InvalidOperationException("get_email_attachment returned no 'size' number.");
        var contentType = stash["contentType"] is JsonValue typeValue && typeValue.TryGetValue<string>(out var type)
            ? type
            : null;
        return new CallToolResult
        {
            Content =
            [
                new TextContentBlock { Text = stashJson },
                new ResourceLinkBlock
                {
                    Uri = EmailAttachmentResource.UriFor(id),
                    Name = name,
                    MimeType = EmailAttachmentResource.MimeTypeFor(name, contentType),
                    Size = size,
                },
            ],
        };
    }

    internal static CallToolResult TextResult(string text) => new() { Content = [new TextContentBlock { Text = text }] };
}
```

- [ ] **Step 4: Wire it into the tool.** Make these edits with the Edit tool.

In `CalendarActionTool.cs`:

1. Tool description. Replace the line

   `        - get_email_attachment: fetch one attachment. Requires accountId, emailId, attachmentId. mode 'stash' (default) or 'inline'.`

   with

   `        - get_email_attachment: fetch one attachment as a resource link (attachment://) plus an attachmentId for send_email. Requires accountId, emailId, attachmentId.`

2. Return type. Replace `    public async Task<string> Calendar(` with `    public async Task<CallToolResult> Calendar(`.

3. `mode` parameter. Delete these two lines:

```csharp
        [Description("get_email_attachment only. 'stash' (default) writes the attachment to the attachment store and returns a handle; 'inline' returns base64 content.")]
        string? mode = null,
```

4. Arguments. In the `new CalendarActionArguments { ... }` initializer, delete the line `                Mode = mode,`.

5. Return statement. Replace

   `            return await DispatchAction(action, new CalendarActionArguments`

   with

   `            var text = await DispatchAction(action, new CalendarActionArguments`

   Then, directly after that initializer's closing `            }).ConfigureAwait(false);`, add:

```csharp
            return action == "get_email_attachment" ? WithAttachmentLink(text) : TextResult(text);
```

6. Dispatch arm. Replace

   `            "get_email_attachment" => GetEmailAttachmentAction(args.AccountId, args.EmailId, args.AttachmentId, args.Mode),`

   with

   `            "get_email_attachment" => GetEmailAttachmentAction(args.AccountId, args.EmailId, args.AttachmentId),`

In `CalendarActionTool.Delegated.cs`, delete this method (it moved to `.Attachments.cs`):

```csharp
    private Task<string> GetEmailAttachmentAction(
        string? accountId, string? emailId, string? attachmentId, string? mode) =>
        Impl<GetEmailAttachmentTool>().GetEmailAttachment(accountId!, emailId!, attachmentId!, mode ?? "stash");

```

In `CalendarActionArguments.cs`, delete `    public string? Mode { get; init; }`.

In `src/CalendarMcp.Core/Skills/attachments.md`, first replace the stdio note:

```
calling `get_email_attachment` in `stash` mode to get an ID for forwarding.
```

with:

```
calling `get_email_attachment` to get an ID for forwarding.
```

Then replace the "Inbound" section: everything from the line `## Inbound (reading/forwarding a file)` up to, but not including, `## Forwarding flow (the most common pattern)`. The new section:

````
## Inbound (reading/forwarding a file)

`get_email_attachment(accountId, emailId, attachmentId)` fetches an
attachment from a received email into the server's attachment store and
returns two things:

```json
{
  "attachmentId": "xyz...",
  "name": "report.pdf",
  "contentType": "application/pdf",
  "size": 4500000,
  "expiresAt": "..."
}
```

plus a resource link, `attachment://xyz...`. A client that needs the
file's content reads that link with `resources/read`; the bytes never
pass through the model. Hand the `attachmentId` to `send_email` to
forward the file. Reading the link does not use up the ID.

````

In the same file, change the forwarding-flow line

`  get_email_attachment(accountId, emailId, attachmentId, mode="stash")`

to

`  get_email_attachment(accountId, emailId, attachmentId)`.

- [ ] **Step 5: Run the tests.** Command as in Task 1 Step 2. Expected: `Passed!`, 0 failed.

  `CalendarActionToolTests.DispatchAction_RoutesEveryPublishedAction` still expects `"attachmentId is required"` for `get_email_attachment`, because the forwarder passes the missing id through to upstream.

- [ ] **Step 6: Commit.**

```bash
cd /d/tmp/aura-pim-mcp
git add src/CalendarMcp.Core/Tools/CalendarActionTool.Attachments.cs src/CalendarMcp.Tests/Tools/CalendarActionToolAttachmentTests.cs
git commit -F - -- src/CalendarMcp.Core/Tools/CalendarActionTool.Attachments.cs src/CalendarMcp.Core/Tools/CalendarActionTool.cs src/CalendarMcp.Core/Tools/CalendarActionTool.Delegated.cs src/CalendarMcp.Core/Tools/CalendarActionArguments.cs src/CalendarMcp.Core/Skills/attachments.md src/CalendarMcp.Tests/Tools/CalendarActionToolAttachmentTests.cs <<'EOF'
feat(calendar): get_email_attachment links the attachment it stashed

The action returned an id that only send_email and the /attachments endpoint
could use, so a client that needed to open the file had no way to get it, and
the inline mode put base64 into the model's context. The curated tool now
returns a CallToolResult: get_email_attachment always stashes and adds a
resource_link to attachment://<id> beside the stash JSON; every other action
returns its JSON as the one text block clients already receive. The link
fails loudly if upstream renames a field it is built from. The inline mode is
gone from the curated schema; upstream's tool class is unchanged.

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
EOF
```

---

### Task 4: Fork notes, merge to `main`, publish

**Files:**
- Modify: `AURA-FORK.md` (item 4, "One curated tool instead of 29")

- [ ] **Step 1: Document the fork difference.** In `AURA-FORK.md`, append this bullet to change 4, after the bullet that begins "**Required value-type parameters are checked by the forwarder.**":

```markdown
   - **Attachments come back as a resource link.** `get_email_attachment` always stashes and
     returns the stash JSON plus a `resource_link` to `attachment://<attachmentId>`, served by
     `EmailAttachmentResource` (tenant from the request principal, non-consuming `TryRead`,
     MIME from the file name when the provider says `application/octet-stream`). The curated
     schema has no `mode`; upstream's tool class keeps its inline mode, unreachable here. The
     per-attachment store cap is 25 MiB.
```

- [ ] **Step 2: Run the full build and suite.**

Run: `MSYS_NO_PATHCONV=1 wsl bash <scratchpad>/pim_build.sh build`, then the same script with `test`.

Expected: `0 Warning(s)`, `0 Error(s)`, then `Passed!` with 0 failed.

- [ ] **Step 3: Smoke-test the stdio server.** Add these two requests to the scratchpad `pim_stdio.sh` probe, after the `tools/call` lines:

```
  echo '{"jsonrpc":"2.0","id":5,"method":"resources/templates/list"}'
  echo '{"jsonrpc":"2.0","id":6,"method":"resources/read","params":{"uri":"attachment://unknown"}}'
```

Then run it.

Expected:
- `resources/templates/list` names `attachment://{attachmentId}`;
- the read of `attachment://unknown` returns the `attachment expired or unknown` error;
- `tools/list` still lists `calendar` only.

- [ ] **Step 4: Commit the notes.**

```bash
cd /d/tmp/aura-pim-mcp
git commit -F - -- AURA-FORK.md <<'EOF'
docs(fork): record the attachment resource link

Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
EOF
```

- [ ] **Step 5: Merge into `main`.**

```bash
cd /d/tmp/aura-pim-mcp
git checkout main
git merge --no-ff aura/upstream-1.8.3 -m "Merge upstream 1.8.3 and the attachment resource link"
```

Expected: a clean merge. `main` has not moved since the branch was cut; check with `git log main..aura/upstream-1.8.3 --oneline` before merging. Then re-run the Step 2 build and test on `main`.

- [ ] **Step 6: Push. ASK THE OPERATOR FIRST.** Pushing `main` publishes `ghcr.io/chetto1983/aura-pim-mcp:sidecar`, and every appliance's updater refreshes to that tag. With the go:

```bash
git push origin main
gh run watch --repo chetto1983/aura-pim-mcp $(gh run list --repo chetto1983/aura-pim-mcp --workflow aura-publish-image.yml --limit 1 --json databaseId -q '.[0].databaseId')
```

Expected: the `aura-publish-image` run finishes green. Also check that the CI run on the same commit is green: `gh run list --repo chetto1983/aura-pim-mcp --limit 3`.
