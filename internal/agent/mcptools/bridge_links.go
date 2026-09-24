package mcptools

import (
	"context"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/mcp"
)

// bridge_links.go reads back the files a tool result linked instead of carrying. A
// resource_link names a resource on the server that returned it, and the one way Aura
// reads it is resources/read on the SAME session that made the call -- an identity
// child's session for an identity-pooled server, so a tenant-scoped resource is read
// as the tenant that asked. Aura never fetches the URI itself, so a hostile link
// cannot make it reach anything on the network.

// resolveLinks turns every link in payload into a FilePart and clears Links.
func resolveLinks(ctx context.Context, session *sdkmcp.ClientSession, payload mcp.ToolPayload) mcp.ToolPayload {
	for _, link := range payload.Links {
		payload.Files = append(payload.Files, readLink(ctx, session, link))
	}
	payload.Links = nil
	return payload
}

// readLink reads one link, unless it declares a size over the cap: that one is never
// fetched. A failure becomes the part's reason, not the call's error, because the
// text of the result is still good.
func readLink(ctx context.Context, session *sdkmcp.ClientSession, link *sdkmcp.ResourceLink) mcp.FilePart {
	if link.Size != nil && *link.Size > mcp.MaxFileBytes {
		return linkPart(link, mcp.FileCapExceeded(*link.Size))
	}
	result, err := mcp.BoundedCall(ctx, func(ctx context.Context) (*sdkmcp.ReadResourceResult, error) {
		return session.ReadResource(ctx, &sdkmcp.ReadResourceParams{URI: link.URI})
	}, nil)
	if err != nil {
		return linkPart(link, "read failed: "+err.Error())
	}
	return linkFile(link, result)
}

// linkFile is the file a link's read returned: the first contents that carry bytes,
// named and typed by the link wherever the contents say less.
func linkFile(link *sdkmcp.ResourceLink, result *sdkmcp.ReadResourceResult) mcp.FilePart {
	if result != nil {
		for _, contents := range result.Contents {
			file, ok := mcp.FileFromContents(contents)
			if !ok {
				continue
			}
			if len(file.Data) > mcp.MaxFileBytes {
				part := linkPart(link, mcp.FileCapExceeded(int64(len(file.Data))))
				part.Size = int64(len(file.Data))
				return part
			}
			part := linkPart(link, "")
			part.MIMEType = preferredMIME(file.MIMEType, link.MIMEType)
			part.Data = file.Data
			return part
		}
	}
	return linkPart(link, "the server returned no contents")
}

// linkPart is a link's FilePart before its bytes arrive: the link's own name, or
// failing that the URI's last segment. The link's advertised size rides along only
// when the bytes are unavailable, the one case Size stands in for them; a part that
// gets its Data is sized by it.
func linkPart(link *sdkmcp.ResourceLink, unavailable string) mcp.FilePart {
	name := link.Name
	if name == "" {
		name = mcp.NameFromURI(link.URI)
	}
	part := mcp.FilePart{Name: name, MIMEType: link.MIMEType, Unavailable: unavailable}
	if unavailable != "" && link.Size != nil {
		part.Size = *link.Size
	}
	return part
}

// preferredMIME picks the more specific of what the contents and the link say. The
// contents win unless they say nothing: the Python SDK fixes one MIME type for every
// read of a resource template, so the link is where such a server states the real one.
func preferredMIME(contents, link string) string {
	for _, candidate := range []string{contents, link} {
		if candidate != "" && candidate != mcp.OctetStream {
			return candidate
		}
	}
	return mcp.OctetStream
}
