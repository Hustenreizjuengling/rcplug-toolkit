// The diagnostics viewer's reading: bundles made up here (never a real
// device's), as rcplug's zip and as the toolkit's .tar.gz fallback.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { deflateRawSync, gzipSync, crc32 } from 'node:zlib';
import { unpack } from '../../web/js/unpack.js';
import { analyze } from '../../web/js/diag.js';

const enc = new TextEncoder();

// zip writes entries the way Go's archive/zip does: deflated, sizes and CRC
// in a data descriptor after the data and in the central directory only.
function zip(entries) {
  const parts = [], central = [];
  let off = 0;
  for (const [name, content] of Object.entries(entries)) {
    const raw = typeof content === 'string' ? enc.encode(content) : content;
    const data = deflateRawSync(raw), n = enc.encode(name), crc = crc32(raw);
    const local = Buffer.alloc(30);
    local.writeUInt32LE(0x04034b50, 0); local.writeUInt16LE(20, 4); local.writeUInt16LE(0x808, 6); local.writeUInt16LE(8, 8);
    local.writeUInt16LE(n.length, 26);
    const desc = Buffer.alloc(16);
    desc.writeUInt32LE(0x08074b50, 0); desc.writeUInt32LE(crc, 4); desc.writeUInt32LE(data.length, 8); desc.writeUInt32LE(raw.length, 12);
    const cd = Buffer.alloc(46);
    cd.writeUInt32LE(0x02014b50, 0); cd.writeUInt16LE(20, 4); cd.writeUInt16LE(20, 6); cd.writeUInt16LE(0x808, 8); cd.writeUInt16LE(8, 10);
    cd.writeUInt32LE(crc, 16); cd.writeUInt32LE(data.length, 20); cd.writeUInt32LE(raw.length, 24); cd.writeUInt16LE(n.length, 28); cd.writeUInt32LE(off, 42);
    parts.push(local, n, data, desc);
    central.push(cd, n);
    off += 30 + n.length + data.length + 16;
  }
  const cdBuf = Buffer.concat(central);
  const end = Buffer.alloc(22);
  end.writeUInt32LE(0x06054b50, 0); end.writeUInt16LE(Object.keys(entries).length, 8); end.writeUInt16LE(Object.keys(entries).length, 10);
  end.writeUInt32LE(cdBuf.length, 12); end.writeUInt32LE(off, 16);
  return Buffer.concat([...parts, cdBuf, end]);
}

function tgz(entries) {
  const blocks = [];
  for (const [name, content] of Object.entries(entries)) {
    const data = enc.encode(content);
    const h = Buffer.alloc(512);
    h.write(name, 0); h.write('0000644\0', 100); h.write(data.length.toString(8).padStart(11, '0') + '\0', 124);
    h.write('0', 156); h.write('ustar\0', 257);
    blocks.push(h, Buffer.from(data), Buffer.alloc((512 - (data.length % 512)) % 512));
  }
  return gzipSync(Buffer.concat([...blocks, Buffer.alloc(1024)]));
}

const bundle = {
  'README.txt': 'rcplug diagnostics\n',
  'rcplug.json': JSON.stringify({
    generated: '2026-01-02T03:04:05Z', version: '9.9.9', hostUptimeSec: 3700,
    health: { memTotalKB: 470000, memAvailableKB: 120000, diskFreeMB: 100, diskTotalMB: 556, load: ['1', '1', '1'] },
    plugins: [
      { name: 'console', title: 'Console', translations: { de: { title: 'Konsole' } }, version: '9.9.9', state: 'running', running: true, enabled: true, restarts: 0 },
      { name: 'demo', title: 'Demo', version: '1.0.0', state: 'crashed', running: false, enabled: true, restarts: 3, lastErr: 'exited (code 2)' },
    ],
  }),
  'problems.txt': '== console.log: 2 line(s)\n   2026-01-02 03:00:00 desk: relay_desync\n   2026-01-02 03:00:01 plugin demo exited (code 2)\n',
  'system/resources-history.txt': 'header text\n\n'
    + '2026-01-02 03:00  CPU  40.0% (30/9/1/0)  load 1 1 1  RAM 300/459 (200 40 26 34)  rcplug 2.0% 25 MB  RØDE 30.0% 180 MB\n'
    + '                  top CPU: rc_audio_mixer 25%\n'
    + '2026-01-02 03:01  CPU  50.5% (30/9/1/0)  load 1 1 1  RAM 310/459 (200 40 26 34)  rcplug 3.5% 26 MB  RØDE 31.0% 181 MB\n',
  'plugins/console/status.json': JSON.stringify({ control: 'basic', connection: { state: 'live', mode: 'relay', since: '2026-01-02T02:00:00Z', stats: { framesIn: 10, framesOut: 2, desyncs: 0 } } }),
  'plugins/console/summary.json': JSON.stringify({ modelName: 'RØDECaster Duo', firmware: '1.7.6', name: '(text)' }),
  'plugins/console/audit.json': JSON.stringify({ writes: [{}, {}] }),
  'plugins/console/changes.json': JSON.stringify({ changes: [{}], dropped: 0 }),
  'logs/update.result': '0 2026-01-02T01:00:00Z\n',
  'logs/update.log': 'rcplug 9.9.9 installed and started.\n  Dashboard: http://10.0.0.7:8080/\n',
  'logs/rcplugd.log': 'listening on 0.0.0.0:8080 and 127.0.0.1:9100\nwlan0 aa:bb:cc:dd:ee:01, serial AB1234567, mail someone@example.org, token ***\n',
  'plugins/console/tree-sanitized.frame': new Uint8Array([0, 1, 2, 3]),
};

test('rcplug\'s zip: overview, history, console, update, files', async () => {
  const files = await unpack(zip(bundle));
  assert.equal(files.size, Object.keys(bundle).length);
  const d = analyze(files);
  assert.equal(d.kind, 'rcplug');
  assert.equal(d.version, '9.9.9');
  assert.deepEqual(d.plugins.map((p) => [p.name, p.running, p.restarts]), [['console', true, 0], ['demo', false, 3]]);
  assert.deepEqual(d.problems.map((p) => [p.file, p.count, p.lines.length]), [['console.log', 2, 2]]);
  assert.equal(d.history.length, 2);
  assert.deepEqual(d.history[1], { t: '2026-01-02 03:01', cpu: 50.5, ramUsed: 310, ramTotal: 459, rcplugCpu: 3.5, rcplugMB: 26, rodeCpu: 31, rodeMB: 181 });
  assert.equal(d.console.model, 'RØDECaster Duo');
  assert.equal(d.console.mode, 'relay');
  assert.equal(d.console.writes, 2);
  assert.equal(d.update.ok, true);
  assert.ok(d.files.some((f) => f.name === 'plugins/console/tree-sanitized.frame' && f.size === 4));
});

test('what identifies a setup is found, masked and local values are not', async () => {
  const d = analyze(await unpack(zip(bundle)));
  const by = Object.fromEntries(d.privacy.map((p) => [p.kind, p]));
  assert.equal(by.ip.count, 1, 'only 10.0.0.7: not 0.0.0.0 or 127.0.0.1');
  assert.deepEqual(by.ip.files, ['logs/update.log']);
  assert.equal(by.mac.count, 1);
  assert.equal(by.serial.count, 1);
  assert.equal(by.email.count, 1);
});

test('the toolkit\'s fallback: raw logs as .tar.gz', async () => {
  const files = await unpack(tgz({ './VERSION': '9.9.8\n', './logs/rcplugd.log': 'ok\npanic: boom\nok\n', './dmesg.txt': 'usb\n' }));
  assert.deepEqual([...files.keys()].sort(), ['VERSION', 'dmesg.txt', 'logs/rcplugd.log']);
  const d = analyze(files);
  assert.equal(d.kind, 'raw');
  assert.equal(d.version, '9.9.8');
  assert.deepEqual(d.problems.map((p) => [p.file, p.lines]), [['rcplugd.log', ['panic: boom']]]);
});

test('files that are not bundles are refused, not trusted', async () => {
  await assert.rejects(unpack(enc.encode('hello')), /not a zip or tar.gz/);
  const z = zip({ 'a.txt': 'x' });
  await assert.rejects(unpack(z.subarray(0, z.length - 30)), /central directory|damaged/);
  // a directory entry claiming more than the limit
  const big = zip({ 'a.txt': 'x' });
  const cd = big.length - 22 - 46 - 5;
  big.writeUInt32LE(200 << 20, cd + 24);
  await assert.rejects(unpack(big), /more than/);
});

test('marking a line uses the same rules as counting', async () => {
  const { hasPrivate } = await import('../../web/js/diag.js');
  assert.equal(hasPrivate('ip', 'listening on 0.0.0.0:8080 and 127.0.0.1:9100'), false);
  assert.equal(hasPrivate('ip', 'client 10.0.0.23'), true);
  assert.equal(hasPrivate('serial', 'serial AB1234567'), true);
});
