# Sample Markdown Document

This is a sample Markdown file used as a conversion fixture for the ingest pipeline.
It tests the round-trip from a plain markdown source to a compiled wiki summary page.

## Features

- Structural coverage of the markdown source type
- Round-trip verification through the ingest pipeline
- Mojibake check on the compiled wiki page body

## Notes

The ingest pipeline stores this file and creates a wiki summary page that links
to the raw markdown via the Extracted Markdown pointer.
