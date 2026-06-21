import { describe, expect, it } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../tabs';

describe('Tabs', () => {
  it('renders the active panel with accent on the active trigger', () => {
    render(
      <Tabs defaultValue="mcp">
        <TabsList>
          <TabsTrigger value="mcp">MCP</TabsTrigger>
          <TabsTrigger value="skills">Skills</TabsTrigger>
        </TabsList>
        <TabsContent value="mcp">mcp-panel</TabsContent>
        <TabsContent value="skills">skills-panel</TabsContent>
      </Tabs>,
    );
    const mcpTab = screen.getByRole('tab', { name: 'MCP' });
    expect(mcpTab.getAttribute('data-state')).toBe('active');
    expect(mcpTab.className).toContain('data-[state=active]:bg-primary');
    expect(mcpTab.className).toContain('min-h-[44px]');
    expect(screen.getByText('mcp-panel')).toBeTruthy();
  });

  it('selects the clicked trigger', async () => {
    render(
      <Tabs defaultValue="mcp">
        <TabsList>
          <TabsTrigger value="mcp">MCP</TabsTrigger>
          <TabsTrigger value="skills">Skills</TabsTrigger>
        </TabsList>
        <TabsContent value="mcp">mcp-panel</TabsContent>
        <TabsContent value="skills">skills-panel</TabsContent>
      </Tabs>,
    );
    const skillsTab = screen.getByRole('tab', { name: 'Skills' });
    expect(skillsTab.getAttribute('aria-selected')).toBe('false');
    // Radix Tabs default to automatic activation (select on focus). Real browsers
    // fire focus on mousedown; jsdom does not, so drive focus directly.
    fireEvent.mouseDown(skillsTab);
    fireEvent.focus(skillsTab);
    await waitFor(() => {
      expect(skillsTab.getAttribute('aria-selected')).toBe('true');
    });
    expect(screen.getByRole('tab', { name: 'MCP' }).getAttribute('aria-selected')).toBe('false');
  });
});
