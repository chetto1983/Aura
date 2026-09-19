import { useState } from 'react';
import { buildComposition } from './project.js';
import { ops } from './editor/ops.js';
import { createHistory } from './editor/history.js';
import { createZundoHistory } from './editor/zundoStore.js';
import { log } from './log.js';

const same = (a, b) => JSON.stringify(a) === JSON.stringify(b);

// Scripted session: add layer, drag (30 events), trim (20 events), change a property, split.
// Each gesture must be ONE undo step; undo must restore the exact snapshot taken before the step,
// redo the one after, and a fresh edit after an undo must clear the redo stack.
async function runHistory(kind) {
  const initial = await buildComposition();
  const h = kind === 'immer' ? createHistory(initial) : createZundoHistory(initial);
  const clip = initial.layers.find((l) => l.settings.name === 'clip').id;
  const photo = initial.layers.find((l) => l.settings.name === 'photo').id;
  const title = initial.layers.find((l) => l.settings.name === 'title').id;
  const snapshots = [h.state];
  const steps = [
    ['add layer', () => h.commit('add', (d) => ops.addLayer(d, { id: 'caption', type: 'text', settings: { enabled: true, startTime: 1, sourceDuration: 2 }, properties: { text: 'nuovo' }, animations: [] }))],
    ['drag clip ×30', () => { h.begin('move'); for (let i = 1; i <= 30; i++) h.commit('move', (d) => ops.move(d, clip, i * 0.02)); h.end(); }],
    ['trim photo end ×20', () => { h.begin('trim'); for (let i = 1; i <= 20; i++) h.commit('trim', (d) => ops.trim(d, photo, 'end', 4 - i * 0.05)); h.end(); }],
    ['title colour', () => h.commit('prop', (d) => ops.setProperty(d, title, 'color', '#e8a33d'))],
    ['split clip @2 s', () => h.commit('split', (d) => ops.split(d, clip, 2))],
  ];
  const timings = [];
  for (const [label, run] of steps) {
    const t0 = performance.now();
    run();
    timings.push({ label, ms: +(performance.now() - t0).toFixed(2) });
    snapshots.push(h.state);
  }
  const checks = [];
  checks.push(['one entry per gesture', h.depth.past === steps.length, h.depth]);
  checks.push(['split produced two clip layers', h.state.layers.filter((l) => l.type === 'video').length === 2]);
  for (let i = steps.length; i > 0; i--) {
    h.undo();
    checks.push([`undo "${steps[i - 1][0]}" restores snapshot ${i - 1}`, same(h.state, snapshots[i - 1])]);
  }
  checks.push(['undo at the bottom is a no-op', (h.undo(), same(h.state, snapshots[0]))]);
  for (let i = 1; i <= steps.length; i++) {
    h.redo();
    checks.push([`redo "${steps[i - 1][0]}" restores snapshot ${i}`, same(h.state, snapshots[i])]);
  }
  h.undo();
  h.undo();
  h.commit('prop', (d) => ops.setProperty(d, title, 'text', 'dopo undo'));
  checks.push(['new edit after undo clears redo', h.depth.future === 0, h.depth]);
  const size = kind === 'immer'
    ? h.serialized().length
    : JSON.stringify(h.pastStates()).length;
  const result = { kind, timings, entries: kind === 'immer' ? h.entries() : undefined, checks: checks.map(([name, ok, extra]) => ({ name, ok, ...(extra ? { extra } : {}) })), pass: checks.every((c) => c[1]), historyChars: size };
  log('history', `${kind}: ${result.pass ? 'PASS' : 'FAIL'}`, result);
  return result;
}

window.__spike = window.__spike ?? {};
window.__spike.history = runHistory;

export default function HistoryLab() {
  const [results, setResults] = useState([]);
  return (
    <section className="lab">
      <div className="row">
        <button onClick={async () => setResults([await runHistory('immer'), await runHistory('zundo')])}>Esegui scenario</button>
      </div>
      {results.map((r) => (
        <div key={r.kind}>
          <strong>{r.kind}: {r.pass ? 'PASS' : 'FAIL'}</strong>
          <ul>{r.checks.map((c) => <li key={c.name}>{c.ok ? '✓' : '✗'} {c.name}</li>)}</ul>
        </div>
      ))}
    </section>
  );
}
