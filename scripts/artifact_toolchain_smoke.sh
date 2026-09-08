#!/usr/bin/env bash
set -euo pipefail

img="${AURA_SANDBOX_IMAGE:-aura-sandbox:latest}"
docker run --rm --network none -i --entrypoint bash "$img" -s <<'SH'
set -euo pipefail
cd /workspace
tsc --version
vite --version
skill=/opt/aura-skills/web-artifacts-builder/scripts
bash "$skill/init-artifact.sh" offline-artifact
cd offline-artifact
cat > src/App.tsx <<'TSX'
import { useState } from 'react'
import { Calendar } from './components/ui/calendar'
import { ResizablePanelGroup, ResizablePanel, ResizableHandle } from './components/ui/resizable'

export default function App() {
  const [count, setCount] = useState(0)
  return <main className="mx-auto max-w-xl p-4">
    <h1>Native artifact check</h1>
    <button onClick={() => setCount(count + 1)}>Count {count}</button>
    <Calendar mode="single" />
    <div className="h-24"><ResizablePanelGroup direction="horizontal">
      <ResizablePanel>Left</ResizablePanel><ResizableHandle />
      <ResizablePanel>Right</ResizablePanel>
    </ResizablePanelGroup></div>
  </main>
}
TSX
bash "$skill/bundle-artifact.sh"
test -s artifact-validation/desktop.png
test -s artifact-validation/mobile.png
python3 - <<'PY'
from pathlib import Path
from playwright.sync_api import sync_playwright, expect
with sync_playwright() as p:
    browser = p.chromium.launch(headless=True)
    page = browser.new_page()
    page.goto(Path('bundle.html').resolve().as_uri())
    page.get_by_role('button', name='Count 0', exact=True).click()
    expect(page.get_by_role('button', name='Count 1', exact=True)).to_be_visible()
    expect(page.get_by_role('grid')).to_be_visible()
    expect(page.get_by_role('separator')).to_be_visible()
    browser.close()
PY

# The same checked bundle must disappear when its replacement fails type checking.
cp src/App.tsx /tmp/valid-artifact-app.tsx
printf '\nconst broken: number = "wrong type";\n' >> src/App.tsx
if bash "$skill/bundle-artifact.sh" > /tmp/expected-build-failure.log 2>&1; then
  echo 'FAIL: an invalid TypeScript project produced a deliverable' >&2
  exit 1
fi
test ! -e bundle.html
cp /tmp/valid-artifact-app.tsx src/App.tsx
printf '\nthrow new Error("browser-gate-probe");\n' >> src/App.tsx
if bash "$skill/bundle-artifact.sh" > /tmp/expected-browser-failure.log 2>&1; then
  echo 'FAIL: a browser exception produced a deliverable' >&2
  exit 1
fi
test ! -e bundle.html
grep -q browser-gate-probe artifact-validation/report.json
echo 'PASS: offline init/build/bundle/browser, calendar/panels, interaction, TypeScript/browser rejection and stale-bundle removal'
SH
