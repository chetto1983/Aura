// StderrLogSheet — right-side Sheet (640px) with npm subprocess stderr tail + Copy action.
// UI-SPEC §Stderr Sheet (line 265-269); triggered by "Vedi log" action on npm_subprocess_fail toast.
import { Sheet, SheetContent, SheetHeader, SheetTitle, SheetDescription } from "./ui/sheet";
import { Button } from "./ui/button";
import { Copy } from "lucide-react";

export interface StderrLogSheetProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  skillName: string;
  source: string;
  exitCode: number;
  stderrTail: string;
}

export function StderrLogSheet({ open, onOpenChange, skillName, source, exitCode, stderrTail }: StderrLogSheetProps) {
  const copy = () => navigator.clipboard?.writeText(stderrTail).catch(() => {});
  return (
    <Sheet open={open} onOpenChange={onOpenChange} modal={false}>
      <SheetContent side="right" className="w-[640px] bg-[--surface] p-0 flex flex-col">
        <SheetHeader className="p-6 pb-3">
          <SheetTitle className="text-[17px] font-semibold text-[--text-strong]">Log installazione {skillName}</SheetTitle>
          <SheetDescription className="text-[12px] text-[--text-dim]">
            Comando: npx skills add {source} · Exit code {exitCode}
          </SheetDescription>
        </SheetHeader>
        <pre className="flex-1 overflow-auto mx-6 p-3 bg-[--surface-sunken] text-[--text-strong] border border-[--border] text-[12.5px] font-mono rounded-[var(--radius)]">
          {stderrTail || "(nessun output)"}
        </pre>
        <div className="p-4 flex justify-end">
          <Button variant="ghost" onClick={copy} className="gap-2">
            <Copy size={14} /> Copia log
          </Button>
        </div>
      </SheetContent>
    </Sheet>
  );
}
