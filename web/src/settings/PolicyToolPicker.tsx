import { useId, useState } from 'react';
import { Check, ChevronDown } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import type { PolicyTool } from '../approvals/useApprovalPolicies';
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from '@/components/ui/command';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import {
  selectItemClass,
  selectMenuClass,
  selectTriggerClass,
} from '@/components/ui/select-classes';
import { cn } from '@/lib/utils';

interface ToolGroup {
  readonly server: string;
  readonly tools: readonly PolicyTool[];
}

// groupByServer puts Aura's own tools first, then one group per MCP server, named by the
// namespace a bridged tool carries before "__" (browser__agent_browser_open → browser).
function groupByServer(tools: readonly PolicyTool[]): ToolGroup[] {
  const groups = new Map<string, PolicyTool[]>();
  for (const tool of tools) {
    const separator = tool.name.indexOf('__');
    const server = separator > 0 ? tool.name.slice(0, separator) : '';
    groups.set(server, [...(groups.get(server) ?? []), tool]);
  }
  return [...groups.entries()]
    .sort(([a], [b]) => (a === '' ? -1 : b === '' ? 1 : a.localeCompare(b)))
    .map(([server, list]) => ({ server, tools: list }));
}

interface PolicyToolPickerProps {
  readonly id: string;
  readonly tools: readonly PolicyTool[];
  readonly value: string;
  readonly onChange: (name: string) => void;
}

// PolicyToolPicker is the tool half of a policy: a searchable list of what the agent can
// call, so the operator picks a name instead of recalling one. A typed name could miss the
// gateway's subject entirely, as "calendar__send_email" did on the lab VM (2026-10-10),
// before the PIM split its one multiplexed tool into one tool per operation.
export function PolicyToolPicker({ id, tools, value, onChange }: PolicyToolPickerProps) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const listId = useId();
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        id={id}
        role="combobox"
        aria-expanded={open}
        aria-haspopup="listbox"
        aria-controls={listId}
        className={selectTriggerClass}
      >
        {value === '' ? (
          <span className="truncate text-text-faint">
            {t('settings.toolPolicies.toolPlaceholder')}
          </span>
        ) : (
          <span className="truncate font-mono">{value}</span>
        )}
        <ChevronDown aria-hidden="true" className="size-4 shrink-0 text-text-muted" />
      </PopoverTrigger>
      <PopoverContent
        align="start"
        sideOffset={6}
        className={cn(selectMenuClass, 'w-(--radix-popover-trigger-width) min-w-80 p-0')}
      >
        <Command className="bg-transparent">
          <CommandInput placeholder={t('settings.toolPolicies.toolSearch')} />
          <CommandList id={listId} className="max-h-72 p-1">
            <CommandEmpty>{t('settings.toolPolicies.toolNone')}</CommandEmpty>
            {groupByServer(tools).map((group) => (
              <CommandGroup
                key={group.server}
                heading={group.server === '' ? t('settings.toolPolicies.builtIn') : group.server}
              >
                {group.tools.map((tool) => (
                  <CommandItem
                    key={tool.name}
                    value={tool.name}
                    keywords={[tool.summary]}
                    data-state={tool.name === value ? 'checked' : 'unchecked'}
                    onSelect={() => {
                      onChange(tool.name);
                      setOpen(false);
                    }}
                    className={selectItemClass}
                  >
                    <span className="flex min-w-0 flex-col">
                      <span className="truncate font-mono">{tool.name}</span>
                      {tool.summary === '' ? null : (
                        <span className="truncate text-[12px] font-normal text-text-muted">
                          {tool.summary}
                        </span>
                      )}
                    </span>
                    {tool.name === value ? (
                      <Check aria-hidden="true" className="absolute right-2 size-3.5" />
                    ) : null}
                  </CommandItem>
                ))}
              </CommandGroup>
            ))}
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}
