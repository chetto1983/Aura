import { describe, expect, it } from 'vitest';
import {
  REDACTED_ARG_VALUE,
  extractWebSearchResults,
  extractWikiPageSummary,
  getArgKeys,
  redactArgsForDisplay,
  resultPreview,
  summarizeArgKeys,
  toolStatusTone,
} from './ToolCallComponent.helpers';

describe('tool call helpers', () => {
  it('summarizes argument keys without exposing values', () => {
    const argsText = JSON.stringify({
      query: 'secret search term',
      max_results: 5,
      api_key: 'sk-secret',
    });

    expect(getArgKeys(undefined, argsText)).toEqual(['api_key', 'max_results', 'query']);
    expect(summarizeArgKeys(undefined, argsText, 2)).toBe('api_key, max_results +1');
    expect(redactArgsForDisplay(undefined, argsText)).toEqual({
      api_key: REDACTED_ARG_VALUE,
      max_results: REDACTED_ARG_VALUE,
      query: REDACTED_ARG_VALUE,
    });
  });

  it('extracts wiki card data from structured tool results', () => {
    expect(extractWikiPageSummary({
      slug: 'phase-cons-notes',
      title: 'Phase CONS Notes',
      body: '# Phase CONS Notes\n\nBody text',
    })).toEqual({
      slug: 'phase-cons-notes',
      title: 'Phase CONS Notes',
      preview: 'Phase CONS Notes',
    });
  });

  it('extracts the first web search results from common result shapes', () => {
    const results = extractWebSearchResults({
      results: [
        { title: 'One', url: 'https://example.com/1', content: 'First' },
        { name: 'Two', href: 'https://example.com/2', description: 'Second' },
      ],
    });

    expect(results).toEqual([
      { title: 'One', url: 'https://example.com/1', snippet: 'First' },
      { title: 'Two', url: 'https://example.com/2', snippet: 'Second' },
    ]);
  });

  it('maps assistant-ui statuses to stable UI tones', () => {
    expect(toolStatusTone('running')).toBe('running');
    expect(toolStatusTone('complete')).toBe('done');
    expect(toolStatusTone('requires-action')).toBe('action');
    expect(toolStatusTone('incomplete', 'error')).toBe('error');
  });

  it('uses backend preview as the compact result summary', () => {
    expect(resultPreview({ preview: '  finished   with   spaces  ' })).toBe('finished with spaces');
  });
});
