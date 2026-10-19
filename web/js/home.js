// The home page: texts and the language switch only.
import { applyTexts, setupLang } from './i18n.js';

setupLang();
applyTexts();

// The setup program of the release these pages come from, this computer's
// system first (toolkit.json is written by tools/sitegen).
const system = (() => {
  const p = ((navigator.userAgentData && navigator.userAgentData.platform) || navigator.platform || navigator.userAgent || '').toLowerCase();
  return p.includes('win') ? 'windows' : p.includes('mac') ? 'macos' : p.includes('linux') ? 'linux' : '';
})();

(async () => {
  let v = '';
  try { v = (await (await fetch('toolkit.json', { cache: 'no-cache' })).json()).version; } catch (e) { return; }
  if (!/^\d+\.\d+\.\d+$/.test(v || '')) return;
  const base = `https://github.com/Hustenreizjuengling/rcplug-toolkit/releases/download/v${v}/`;
  const files = [
    ['windows', 'Windows 10/11', `rcplug-toolkit-${v}-windows.zip`],
    ['macos', 'macOS (Apple silicon)', `rcplug-setup-${v}-macos-arm64`],
    ['macos', 'macOS (Intel)', `rcplug-setup-${v}-macos-amd64`],
    ['linux', 'Linux (x86-64)', `rcplug-setup-${v}-linux-amd64`],
    ['linux', 'Linux (ARM64)', `rcplug-setup-${v}-linux-arm64`],
  ];
  files.sort((a, b) => (b[0] === system) - (a[0] === system));
  const links = files.map(([os, label, name]) => {
    const a = document.createElement('a');
    a.className = 'btn' + (os === system ? ' primary' : '');
    a.href = base + name;
    a.innerHTML = '<svg class="i i-sm" aria-hidden="true"><use href="ui/icons.svg#download"/></svg>';
    a.append(label);
    return a;
  });
  const sums = document.createElement('a');
  sums.className = 'btn ghost';
  sums.href = base + 'SHA256SUMS';
  sums.textContent = 'SHA256SUMS';
  document.querySelector('#dlLinks').replaceChildren(...links, sums);
  document.querySelector('#dlVer').textContent = 'rcplug-toolkit ' + v;
  document.querySelector('#dl').hidden = false;
})();
