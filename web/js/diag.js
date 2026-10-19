// What a diagnostics bundle says, read from its files (no page, no DOM): the
// viewer shows it, the tests check it. Understands rcplug's bundle (see its
// README.txt) and the toolkit's fallback with only the raw logs.
import { text } from './unpack.js';

const json = (files, name) => {
  try { return files.has(name) ? JSON.parse(text(files.get(name))) : null; } catch (e) { return null; }
};

// The lines worth a look in a log, as rcplugd's own problems.txt finds them
// (internal/host/diagnostics.go); the console refusing a write the control
// level does not allow is the console doing its job.
const problemLine = /panic:|fatal error|SIGSEGV|segmentation|exited \(|restarting in|out of memory|guard:|desync|refused|failed/i;
const policyRefusal = /console: write .* refused: /;

export function scanProblems(name, log) {
  const hits = log.split('\n').filter((l) => problemLine.test(l) && !policyRefusal.test(l));
  return hits.length ? { file: name, count: hits.length, lines: hits.slice(-30) } : null;
}

// The minute lines of system/resources-history.txt.
const historyLine = /^(\d{4}-\d\d-\d\d \d\d:\d\d)\s+CPU\s+([\d.]+)%.*?RAM (\d+)\/(\d+).*?rcplug ([\d.]+)% (\d+) MB\s+\S+ ([\d.]+)% (\d+) MB/;

export function parseHistory(t) {
  const out = [];
  for (const l of t.split('\n')) {
    const m = historyLine.exec(l);
    if (m) out.push({ t: m[1], cpu: +m[2], ramUsed: +m[3], ramTotal: +m[4], rcplugCpu: +m[5], rcplugMB: +m[6], rodeCpu: +m[7], rodeMB: +m[8] });
  }
  return out;
}

// What can still identify a setup, for a look before sharing. Masked values
// (***) are not counted.
const privacyChecks = [
  // addresses of machines (not loopback, wildcard, broadcast or multicast)
  ['ip', /\b(?:\d{1,3}\.){3}\d{1,3}\b/g, (v) => { const o = v.split('.').map(Number); return o.every((x) => x <= 255) && o[0] !== 0 && o[0] !== 127 && o[0] < 224; }],
  ['mac', /\b[0-9a-f]{2}(?::[0-9a-f]{2}){5}\b/gi, (v) => !/^(00:){5}00$/i.test(v)],
  ['email', /\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b/g, () => true],
  ['serial', /\b[A-Z]{2}\d{7}\b/g, () => true],
];

// hasPrivate tells whether a line holds something of the given kind.
export function hasPrivate(kind, line) {
  const c = privacyChecks.find(([k]) => k === kind);
  return !!c && [...line.matchAll(c[1])].some((m) => c[2](m[0]));
}

export function privacy(files) {
  const found = {};
  for (const [name, bytes] of files) {
    if (bytes.length > 4 << 20 || name.endsWith('.frame')) continue; // binary
    const t = text(bytes);
    for (const [kind, re, keep] of privacyChecks) {
      for (const m of t.matchAll(re)) {
        if (!keep(m[0])) continue;
        const f = (found[kind] ||= { kind, values: new Set(), files: new Set() });
        f.values.add(m[0]);
        f.files.add(name);
      }
    }
  }
  return Object.values(found).map((f) => ({ kind: f.kind, count: f.values.size, files: [...f.files].sort() }));
}

// analyze turns the files into what the viewer shows.
export function analyze(files) {
  const info = json(files, 'rcplug.json');
  const d = {
    kind: info ? 'rcplug' : 'raw',
    version: info ? info.version : text(files.get('VERSION')).trim(),
    generated: info && info.generated, note: info && info.note,
    hostUptimeSec: info && info.hostUptimeSec,
    health: (info && info.health) || null,
    plugins: ((info && info.plugins) || []).map((p) => ({
      name: p.name, title: p.title, translations: p.translations, version: p.version, state: p.state,
      running: !!p.running, enabled: !!p.enabled, restarts: p.restarts || 0, lastErr: p.lastErr || '',
    })),
    invalid: (info && info.invalid) || [],
    problems: [], history: [], console: null, update: null,
    files: [...files].map(([name, b]) => ({ name, size: b.length })).sort((a, b) => a.name.localeCompare(b.name)),
  };
  // Problems: rcplugd's own list, else found here in the raw logs.
  if (!files.has('problems.txt')) {
    for (const [name, b] of files) {
      if (/(^|\/)logs\/[^/]+$|\.log$/.test(name) && !name.startsWith('logs/memory/')) {
        const p = scanProblems(name.replace(/^.*logs\//, ''), text(b));
        if (p) d.problems.push(p);
      }
    }
  } else {
    let cur = null;
    for (const l of text(files.get('problems.txt')).split('\n')) {
      const m = /^== (.+): (\d+) line/.exec(l);
      if (m) { cur = { file: m[1], count: +m[2], lines: [] }; d.problems.push(cur); } else if (cur && /^ {3}\S/.test(l) && !/^ {3}\(last \d+ shown\)$/.test(l)) cur.lines.push(l.slice(3));
    }
  }
  if (files.has('system/resources-history.txt')) d.history = parseHistory(text(files.get('system/resources-history.txt')));
  d.resources = text(files.get('system/resources.txt'));
  // The console: what it is connected to and how.
  const status = json(files, 'plugins/console/status.json');
  const summary = json(files, 'plugins/console/summary.json');
  if (status || summary) {
    const c = (status && status.connection) || {};
    d.console = {
      model: (summary && summary.modelName) || (status && status.modelName) || '',
      firmware: (summary && summary.firmware) || (status && status.firmware) || '',
      state: c.state || '', mode: c.mode || '', since: c.since || '', error: c.error || c.lastError || '',
      control: status && status.control, readOnly: !!(status && (status.readOnly || status.readOnlyFlag)),
      stats: c.stats || null,
      writes: ((json(files, 'plugins/console/audit.json') || {}).writes || []).length,
      changes: ((json(files, 'plugins/console/changes.json') || {}).changes || []).length,
    };
  }
  // The last update of rcplug: the installer's result and log.
  if (files.has('logs/update.result') || files.has('logs/update.log')) {
    const r = /^(\d+)\s+(\S+)/.exec(text(files.get('logs/update.result')).trim());
    d.update = { ok: r ? r[1] === '0' : null, at: r ? r[2] : '', log: text(files.get('logs/update.log')).trim().split('\n').slice(-12) };
  }
  d.privacy = privacy(files);
  return d;
}
