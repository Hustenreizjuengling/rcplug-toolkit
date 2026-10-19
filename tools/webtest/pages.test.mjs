// The web pages hang together: every text they name exists in both
// languages, every file they reference is in web/ or made by tools/sitegen.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, existsSync, readdirSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const web = join(dirname(fileURLToPath(import.meta.url)), '../../web');
const generated = new Set(['ui/rcplug.css', 'ui/icons.svg', 'rcplug-web.wasm', 'wasm_exec.js', 'firmware.json', 'toolkit.json']);
const pages = readdirSync(web).filter((f) => f.endsWith('.html'));
const scripts = readdirSync(join(web, 'js')).map((f) => 'js/' + f);

// i18n.js reads localStorage and the document: give it what a page has.
globalThis.localStorage = { getItem: () => null, setItem() {} };
globalThis.navigator ??= { language: 'en' };
globalThis.document = { documentElement: {}, querySelectorAll: () => [], querySelector: () => null };
const { keys } = await import('../../web/js/i18n.js');

test('both languages have the same texts', () => {
  assert.deepEqual([...keys('de')].sort(), [...keys('en')].sort());
});

test('every text a page or script names exists', () => {
  const en = new Set(keys('en'));
  for (const f of pages) {
    for (const [, k] of readFileSync(join(web, f), 'utf8').matchAll(/data-t(?:-ph|-html)?="([^"]+)"/g)) assert.ok(en.has(k), `${f}: ${k}`);
  }
  for (const f of scripts) {
    for (const [, k] of readFileSync(join(web, f), 'utf8').matchAll(/\bt\('([A-Za-z0-9]+)'/g)) assert.ok(en.has(k), `${f}: ${k}`);
  }
});

test('every referenced file exists or is generated', () => {
  const refs = (text) => [...text.matchAll(/(?:href|src)="([^"]+)"/g), ...text.matchAll(/(?:fetch\(|s\.src = )'([^']+)'/g), ...text.matchAll(/\bfrom '([^']+)'/g)]
    .map((m) => m[1].replace(/[?#].*$/, '')).filter((r) => r && !/^[a-z]+:/.test(r)); // not https:, data:, …
  for (const f of [...pages, ...scripts]) {
    for (const r of refs(readFileSync(join(web, f), 'utf8'))) {
      if (r === './') continue;
      // a module import is relative to its script; a fetch or a script element to the page
      const p = r.replace(/^\.\//, '');
      const inJs = f.startsWith('js/') && existsSync(join(web, 'js', p));
      assert.ok(generated.has(p) || existsSync(join(web, p)) || inJs, `${f} references ${r}`);
    }
  }
});

test('the pages forbid what they do not need', () => {
  for (const f of pages) {
    const csp = readFileSync(join(web, f), 'utf8').match(/http-equiv="Content-Security-Policy" content="([^"]+)"/);
    assert.ok(csp, f + ': no CSP');
    assert.match(csp[1], /default-src 'self'/);
    assert.doesNotMatch(csp[1], /'unsafe-eval'|script-src[^;]*\*/, f); // WebAssembly needs only 'wasm-unsafe-eval'
  }
});
