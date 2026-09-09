import { makePrismAsyncLightSyntaxHighlighter } from '@assistant-ui/react-syntax-highlighter';
import type { SyntaxHighlighterProps } from '@assistant-ui/react-markdown';
import { oneDark, oneLight } from 'react-syntax-highlighter/dist/esm/styles/prism';
import { useThemeMode } from '@/theme/useThemeMode';

// codeHighlighter — the SyntaxHighlighter slot MarkdownTextPrimitive has always exposed and
// Aura never filled, which is why every fenced block rendered as plain grey text (operator,
// 2026-08-17). It is the library's own Prism binding, not a hand-rolled tokenizer.
//
// `PrismAsyncLight` loads one language grammar on demand instead of bundling all ~300: a
// chat can contain any language, and the whole point of the async-light build is that the
// bundle does not pay for the ones it never sees.
//
// The background is deliberately transparent so the surrounding <pre> keeps owning the
// chrome (border, radius, surface token) — otherwise the theme's own slab paints over
// Aura's surface and the block stops matching every other card on the page.
const HIGHLIGHTER_CONFIG = {
  customStyle: { background: 'transparent', margin: 0, padding: 0, fontSize: 'inherit' },
  codeTagProps: { style: { background: 'transparent', fontFamily: 'inherit' } },
} as const;

const DarkHighlighter = makePrismAsyncLightSyntaxHighlighter({
  ...HIGHLIGHTER_CONFIG,
  style: oneDark,
});
const LightHighlighter = makePrismAsyncLightSyntaxHighlighter({
  ...HIGHLIGHTER_CONFIG,
  style: oneLight,
});

// The style objects are baked into the component at construction, so the theme cannot be a
// prop -- it has to pick a different component. useThemeMode is what keeps that choice live
// across a theme switch.
export function CodeHighlighter(props: SyntaxHighlighterProps) {
  const theme = useThemeMode();
  const Highlighter = theme === 'light' ? LightHighlighter : DarkHighlighter;
  return <Highlighter {...props} />;
}
