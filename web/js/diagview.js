// The diagnostics viewer: a bundle from rcplug (or the toolkit's fallback
// with the raw logs) opened in this tab, shown as an overview. Nothing is
// uploaded.
import { t, lang, applyTexts, setupLang } from './i18n.js';
import { unpack, text } from './unpack.js';
import { analyze, hasPrivate } from './diag.js';

const $ = (s) => document.querySelector(s);
const el = (tag, cls, txt) => { const e = document.createElement(tag); if (cls) e.className = cls; if (txt !== undefined) e.textContent = txt; return e; };
const pill = (cls, txt) => el('span', 'pill ' + cls, txt);
let files = null;

setupLang();
applyTexts();
$('#filter').setAttribute('aria-label', t('dFilter'));

// ---- opening a file
async function open(f) {
  if (!f) return;
  $('#err').replaceChildren();
  $('#dropText').textContent = t('dReading', { name: f.name });
  try {
    files = await unpack(await f.arrayBuffer());
    if (!files.size) throw new Error(t('dEmpty'));
    show(analyze(files));
    $('#dropText').textContent = f.name + ' · ' + size(f.size);
    $('#drop').classList.add('ok');
  } catch (e) {
    $('#view').hidden = true;
    $('#drop').classList.remove('ok');
    $('#dropText').textContent = t('dDrop');
    $('#err').replaceChildren(el('div', 'banner danger', t('failed', { err: e.message })));
  }
}
$('#file').addEventListener('change', (e) => open(e.target.files[0]));
const drop = $('#drop');
drop.addEventListener('dragover', (e) => { e.preventDefault(); drop.classList.add('over'); });
drop.addEventListener('dragleave', () => drop.classList.remove('over'));
drop.addEventListener('drop', (e) => { e.preventDefault(); drop.classList.remove('over'); open(e.dataTransfer.files[0]); });

// ---- formatting
const dur = (s) => {
  if (s == null) return '–';
  const d = Math.floor(s / 86400), h = Math.floor((s % 86400) / 3600), m = Math.floor((s % 3600) / 60);
  return d ? `${d} d ${h} h` : h ? `${h} h ${m} min` : `${m} min`;
};
const when = (iso) => { const d = new Date(iso); return isNaN(d) ? iso || '–' : d.toLocaleString(lang); };
const mb = (kb) => Math.round(kb / 1024);

function stat(label, value, sub) {
  const s = el('div', 'stat card');
  s.append(el('div', 'label', label), el('div', 'value', value));
  if (sub) s.append(el('div', 'dim small', sub));
  return s;
}

// ---- the overview
function show(d) {
  const h = d.health || {};
  const c = d.console;
  $('#stats').replaceChildren(
    stat('rcplug', d.version || '?', d.kind === 'raw' ? t('dRaw') : ''),
    stat(t('dDevice'), c && c.model ? c.model : '–', c && c.firmware ? t('dFirmware', { v: c.firmware }) : ''),
    stat(t('dMade'), d.generated ? when(d.generated) : '–', d.hostUptimeSec != null ? t('dRunning', { d: dur(d.hostUptimeSec) }) : ''),
    stat(t('dRAM'), h.memTotalKB ? `${mb(h.memAvailableKB)} MB` : '–', h.memTotalKB ? t('dFreeOf', { n: mb(h.memTotalKB) }) : ''),
    stat(t('dDisk'), h.diskTotalMB ? `${h.diskFreeMB} MB` : '–', h.diskTotalMB ? t('dFreeOf', { n: h.diskTotalMB }) : ''),
  );
  $('#note').replaceChildren(...(d.note ? [el('div', 'banner info', d.note)] : []));
  showProblems(d);
  showHistory(d);
  showPlugins(d);
  showConsole(d);
  showUpdate(d);
  showPrivacy(d);
  showFiles(d);
  $('#view').hidden = false;
}

function showProblems(d) {
  const n = d.problems.reduce((a, p) => a + p.count, 0);
  $('#problemsPill').replaceChildren(n ? pill('warn', t('dLines', { n })) : pill('ok', t('dNone')));
  if (!n) { $('#problems').replaceChildren(el('p', 'muted', t('dNoProblems'))); return; }
  $('#problems').replaceChildren(...d.problems.map((p) => {
    const det = el('details', 'prob');
    const sum = el('summary');
    sum.append(el('b', 'mono', p.file), ' ', el('span', 'dim', t('dLines', { n: p.count })));
    det.append(sum, el('pre', 'logs', p.lines.join('\n')));
    return det;
  }));
}

// A small chart of the device over the last hours: CPU of the whole device,
// of RØDE's programs and of rcplug, and the RAM in use.
function showHistory(d) {
  const hs = d.history;
  $('#historyCard').hidden = hs.length < 2;
  if (hs.length < 2) return;
  $('#historySpan').textContent = `${hs[0].t.slice(11)} – ${hs[hs.length - 1].t.slice(11)}`;
  const W = 720, H = 150, L = 34, R = 8, T = 8, B = 20;
  const x = (i) => L + ((W - L - R) * i) / (hs.length - 1);
  const y = (v, max) => T + (H - T - B) * (1 - v / max);
  const line = (vals, max) => vals.map((v, i) => `${x(i).toFixed(1)},${y(v, max).toFixed(1)}`).join(' ');
  const ramMax = hs[0].ramTotal || Math.max(...hs.map((p) => p.ramUsed));
  const svgns = 'http://www.w3.org/2000/svg';
  const svg = document.createElementNS(svgns, 'svg');
  svg.setAttribute('viewBox', `0 0 ${W} ${H}`);
  svg.setAttribute('class', 'chart');
  svg.setAttribute('role', 'img');
  svg.setAttribute('aria-label', t('dHistory'));
  const add = (tag, attrs, txt) => { const e = document.createElementNS(svgns, tag); for (const k in attrs) e.setAttribute(k, attrs[k]); if (txt) e.textContent = txt; svg.append(e); return e; };
  for (const v of [0, 50, 100]) {
    add('line', { x1: L, x2: W - R, y1: y(v, 100), y2: y(v, 100), class: 'grid' });
    add('text', { x: L - 6, y: y(v, 100) + 4, 'text-anchor': 'end', class: 'axis' }, v + '%');
  }
  const step = Math.max(1, Math.round(hs.length / 6));
  for (let i = 0; i < hs.length; i += step) add('text', { x: x(i), y: H - 4, 'text-anchor': 'middle', class: 'axis' }, hs[i].t.slice(11));
  add('polyline', { points: line(hs.map((p) => p.ramUsed), ramMax), class: 'ram' });
  add('polyline', { points: line(hs.map((p) => p.cpu), 100), class: 'cpu' });
  add('polyline', { points: line(hs.map((p) => p.rodeCpu), 100), class: 'rode' });
  add('polyline', { points: line(hs.map((p) => p.rcplugCpu), 100), class: 'rcplug' });
  const last = hs[hs.length - 1];
  const legend = el('div', 'legend');
  for (const [cls, label] of [['cpu', t('dCPU', { v: last.cpu })], ['rode', t('dRode', { v: last.rodeCpu })], ['rcplug', t('dRcplug', { v: last.rcplugCpu, m: last.rcplugMB })], ['ram', t('dRAMUsed', { v: last.ramUsed, of: last.ramTotal })]]) {
    const item = el('span', 'item');
    item.append(el('i', cls), label);
    legend.append(item);
  }
  $('#history').replaceChildren(svg, legend);
}

const tr = (p) => (p.translations && p.translations[lang] && p.translations[lang].title) || p.title || p.name;

function showPlugins(d) {
  if (!d.plugins.length) { $('#plugins').replaceChildren(el('p', 'muted', t('dNoPlugins'))); return; }
  const tbl = el('table', 'table');
  const head = el('tr');
  for (const k of ['dPlugin', 'dVersion', 'dState', 'dRestarts']) head.append(el('th', '', t(k)));
  tbl.append(head);
  for (const p of d.plugins) {
    const row = el('tr');
    const name = el('td');
    name.append(el('b', '', tr(p)));
    if (p.lastErr) name.append(el('div', 'error small', p.lastErr));
    const state = el('td');
    state.append(pill(p.running ? 'ok' : p.enabled ? 'danger' : 'plain', p.running ? t('dRunningState') : p.enabled ? t('dStopped') : t('dOff')));
    row.append(name, el('td', 'num', p.version || ''), state, el('td', 'num' + (p.restarts ? ' warn-text' : ''), String(p.restarts)));
    tbl.append(row);
  }
  $('#plugins').replaceChildren(tbl);
  if (d.invalid.length) $('#plugins').append(el('div', 'banner warn', t('dInvalid', { list: d.invalid.join(', ') })));
}

function showConsole(d) {
  const c = d.console;
  $('#consoleCard').hidden = !c;
  if (!c) return;
  const mode = { relay: t('dModeRelay'), tap: t('dModeTap'), connect: t('dModeConnect') }[c.mode] || c.mode || '–';
  const s = c.stats || {};
  const rows = [
    [t('dConnection'), c.state === 'live' ? t('dLive') : c.state || '–'],
    [t('dMode'), mode],
    [t('dSince'), c.since ? when(c.since) : '–'],
    [t('dControl'), (c.control || '–') + (c.readOnly ? ' · ' + t('dReadOnly') : '')],
    [t('dTraffic'), t('dFrames', { i: s.framesIn ?? '–', o: s.framesOut ?? '–' })],
    [t('dErrors'), t('dErrorCounts', { d: s.desyncs ?? 0, e: s.decodeErrors ?? 0, a: s.applyErrors ?? 0 })],
    [t('dWrites'), t('dWriteCounts', { w: c.writes, c: c.changes })],
  ];
  if (c.error) rows.push([t('dLastError'), c.error]);
  $('#console').replaceChildren(kv(rows));
}

function kv(rows) {
  const dl = el('dl', 'kv');
  for (const [k, v] of rows) dl.append(el('dt', '', k), el('dd', '', v));
  return dl;
}

function showUpdate(d) {
  const u = d.update;
  $('#updateCard').hidden = !u;
  if (!u) return;
  $('#updatePill').replaceChildren(u.ok === null ? pill('plain', '?') : pill(u.ok ? 'ok' : 'danger', u.ok ? t('dUpdateOk') : t('dUpdateFailed')));
  const out = [];
  if (u.at) out.push(el('p', 'dim small', when(u.at)));
  out.push(el('pre', 'logs', u.log.join('\n')));
  $('#update').replaceChildren(...out);
}

function showPrivacy(d) {
  if (!d.privacy.length) { $('#privacy').replaceChildren(el('div', 'banner ok', t('dPrivacyNone'))); return; }
  $('#privacy').replaceChildren(...d.privacy.map((p) => {
    const row = el('div', 'privrow');
    const head = el('div');
    head.append(el('b', '', t('dPriv_' + p.kind, { n: p.count })));
    row.append(head);
    const where = el('div', 'cluster');
    for (const f of p.files) {
      const b = el('button', 'btn sm ghost mono', f);
      b.type = 'button';
      b.onclick = () => openFile(f, p.kind);
      where.append(b);
    }
    row.append(where);
    return row;
  }));
}

// ---- the files themselves
function showFiles(d) {
  $('#filesCount').textContent = t('dFileCount', { n: d.files.length });
  $('#files').replaceChildren(...d.files.map((f) => {
    const b = el('button', 'fileitem');
    b.type = 'button';
    b.dataset.name = f.name;
    b.setAttribute('role', 'option');
    b.append(el('span', 'mono', f.name), el('span', 'dim num', size(f.size)));
    b.onclick = () => openFile(f.name);
    return b;
  }));
  const first = ['problems.txt', 'README.txt', 'system/resources.txt'].find((n) => files.has(n)) || (d.files[0] && d.files[0].name);
  if (first) openFile(first);
}
const size = (n) => (n >= 1048576 ? (n / 1048576).toFixed(1) + ' MB' : n >= 1024 ? Math.round(n / 1024) + ' KB' : n + ' B');

let shown = '';
function openFile(name, mark) {
  shown = name;
  for (const b of document.querySelectorAll('#files .fileitem')) b.setAttribute('aria-selected', String(b.dataset.name === name));
  $('#fileName').textContent = name;
  $('#filter').value = '';
  render(mark);
  if (mark) $('#filesCard').scrollIntoView({ behavior: 'smooth', block: 'start' });
}

function render(mark) {
  const b = files.get(shown);
  let body;
  if (!b) body = '';
  else if (/\.frame$|\.bin$/.test(shown)) body = t('dBinary', { n: size(b.length) });
  else {
    body = text(b);
    if (/\.json$/.test(shown)) { try { body = JSON.stringify(JSON.parse(body), null, 2); } catch (e) { /* as it is */ } }
  }
  const q = $('#filter').value.trim().toLowerCase();
  const lines = body.split('\n');
  const keep = q ? lines.filter((l) => l.toLowerCase().includes(q)) : mark ? lines.filter((l) => hasPrivate(mark, l)) : lines;
  if (mark && !q) $('#filter').placeholder = t('dMarked');
  $('#fileText').textContent = keep.join('\n') || t('dNoMatch');
}
$('#filter').addEventListener('input', () => { $('#filter').placeholder = t('dFilter'); render(); });
