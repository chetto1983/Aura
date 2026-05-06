import fs from 'node:fs';
import path from 'node:path';
import process from 'node:process';

const root = process.cwd();
const localeDir = path.join(root, 'src', 'i18n', 'locales');
const sourceDir = path.join(root, 'src');
const locales = ['en', 'it'];

function readJson(file) {
  return JSON.parse(fs.readFileSync(file, 'utf8'));
}

function walk(dir, out = []) {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) walk(full, out);
    else if (/\.(ts|tsx|js|jsx)$/.test(entry.name)) out.push(full);
  }
  return out;
}

function placeholders(value) {
  return new Set(Array.from(value.matchAll(/\{\{([A-Za-z][A-Za-z0-9_]*)\}\}/g), (m) => m[1]));
}

function hasKey(dict, key) {
  return Object.hasOwn(dict, key) || Object.hasOwn(dict, `${key}_one`) || Object.hasOwn(dict, `${key}_other`);
}

const dictionaries = Object.fromEntries(
  locales.map((locale) => [locale, readJson(path.join(localeDir, `${locale}.json`))]),
);
const keySets = Object.fromEntries(
  locales.map((locale) => [locale, new Set(Object.keys(dictionaries[locale]))]),
);
const allKeys = new Set(locales.flatMap((locale) => Array.from(keySets[locale])));
const failures = [];

for (const locale of locales) {
  const missing = Array.from(allKeys).filter((key) => !keySets[locale].has(key));
  for (const key of missing) failures.push(`${locale}: missing locale key ${key}`);
}

for (const key of allKeys) {
  const reference = dictionaries.en[key];
  if (typeof reference !== 'string') continue;
  const expected = placeholders(reference);
  for (const locale of locales.filter((l) => l !== 'en')) {
    const translated = dictionaries[locale][key];
    if (typeof translated !== 'string') continue;
    const actual = placeholders(translated);
    const missing = Array.from(expected).filter((name) => !actual.has(name));
    const extra = Array.from(actual).filter((name) => !expected.has(name));
    for (const name of missing) failures.push(`${locale}:${key}: missing interpolation {{${name}}}`);
    for (const name of extra) failures.push(`${locale}:${key}: unexpected interpolation {{${name}}}`);
  }
}

for (const locale of locales) {
  for (const [key, value] of Object.entries(dictionaries[locale])) {
    if (typeof value !== 'string') continue;
    if (/(?<!\{)\{[A-Za-z][A-Za-z0-9_]*\}(?!\})/.test(value)) {
      failures.push(`${locale}:${key}: uses single-brace interpolation`);
    }
  }
}

const usedKeys = new Set();
for (const file of walk(sourceDir)) {
  const text = fs.readFileSync(file, 'utf8');
  for (const match of text.matchAll(/\bt\(\s*['"]([^'"]+)['"]/g)) {
    usedKeys.add(match[1]);
  }
}

for (const key of usedKeys) {
  for (const locale of locales) {
    if (!hasKey(dictionaries[locale], key)) {
      failures.push(`${locale}: code uses missing translation key ${key}`);
    }
  }
}

if (failures.length > 0) {
  console.error(`i18n audit failed with ${failures.length} finding(s):`);
  for (const failure of failures) console.error(`- ${failure}`);
  process.exit(1);
}

console.log(`i18n audit passed: ${locales.length} locales, ${allKeys.size} keys, ${usedKeys.size} direct t() keys.`);
