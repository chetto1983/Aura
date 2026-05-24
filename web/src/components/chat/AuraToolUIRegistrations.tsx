import { makeAssistantToolUI } from '@assistant-ui/react';
import { WebSearchCard } from './tools/WebSearchCard';
import { WikiPageCard } from './tools/WikiPageCard';

const WikiPageToolUI = makeAssistantToolUI<Record<string, unknown>, unknown>({
  toolName: 'wiki_page',
  render: (props) => <WikiPageCard {...props} />,
});

const WebSearchToolUI = makeAssistantToolUI<Record<string, unknown>, unknown>({
  toolName: 'web_search',
  render: (props) => <WebSearchCard {...props} />,
});

const WebToolSearchUI = makeAssistantToolUI<Record<string, unknown>, unknown>({
  toolName: 'web',
  render: (props) => <WebSearchCard {...props} />,
});

export function AuraToolUIRegistrations() {
  return (
    <>
      <WikiPageToolUI />
      <WebSearchToolUI />
      <WebToolSearchUI />
    </>
  );
}
