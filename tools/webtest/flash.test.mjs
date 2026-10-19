// The browser flash procedure (web/js/flash.js) against a simulated Duo and
// update disk: the reports it sends, the acks it accepts, the devices and
// disks it refuses, the files it writes.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import * as flash from '../../web/js/flash.js';

// A Duo's HID interface as WebHID shows it: the report descriptor of
// firmware 1.7.6 (report 1 OUT 63 bytes, 2 IN 63, 3 OUT and 4 IN 255).
class FakeDuo extends EventTarget {
  constructor(productId = 0x0050, { ack = true, notify = true } = {}) {
    super();
    Object.assign(this, { vendorId: 0x19f7, productId, productName: 'RØDECaster Duo', opened: false, sent: [], ack, notify });
    this.collections = [{
      usagePage: 0xff00, usage: 1,
      outputReports: [{ reportId: 1, items: [{ reportSize: 8, reportCount: 63 }] }, { reportId: 3, items: [{ reportSize: 8, reportCount: 255 }] }],
      inputReports: [{ reportId: 2, items: [{ reportSize: 8, reportCount: 63 }] }, { reportId: 4, items: [{ reportSize: 8, reportCount: 255 }] }],
    }];
  }
  async open() { this.opened = true; }
  async close() { this.opened = false; }
  async sendReport(id, data) {
    if (!this.opened) throw new Error('not open');
    this.sent.push({ id, bytes: Array.from(data) });
    const report = (reportId, first) => {
      const b = new Uint8Array(reportId === 4 ? 255 : 63);
      b[0] = first;
      this.dispatchEvent(Object.assign(new Event('inputreport'), { reportId, data: new DataView(b.buffer) }));
    };
    setTimeout(() => {
      if (this.notify) report(4, 0x41); // a property notification, not an answer
      if (this.ack) report(2, 0x41);
    }, 5);
  }
}

const hid = (...devices) => ({ chooser: [], async requestDevice(o) { this.chooser.push(o.filters); return devices.length ? [devices.shift()] : []; } });

// A folder as the File System Access API hands it out, in memory.
class FakeDir {
  constructor(name, entries = {}) { this.name = name; this.files = new Map(Object.entries(entries).map(([k, v]) => [k, new Blob([v])])); this.gone = false; }
  async *keys() { yield* this.files.keys(); }
  async getFileHandle(name, o = {}) {
    if (this.gone) throw new DOMException('gone', 'NotFoundError');
    if (!this.files.has(name) && !o.create) throw new DOMException(name, 'NotFoundError');
    const dir = this;
    return {
      async createWritable() {
        const parts = [];
        return { async write(b) { parts.push(b); }, async close() { dir.files.set(name, new Blob(parts)); }, async abort() {} };
      },
      async getFile() { return dir.files.get(name) || new Blob([]); },
    };
  }
}

test('M and U: report 1, 63 bytes, the command first; the ack is report 2 with A', async () => {
  const d = new FakeDuo();
  assert.equal(flash.reportLength(d), 63);
  assert.equal(await flash.command(d, flash.CMD_ENTER_UPDATE), true);
  assert.equal(d.sent.length, 1);
  assert.equal(d.sent[0].id, 1);
  assert.equal(d.sent[0].bytes.length, 63);
  assert.equal(d.sent[0].bytes[0], 0x4d);
  assert.ok(d.sent[0].bytes.slice(1).every((b) => b === 0));
  assert.equal(await flash.command(new FakeDuo(0x004f), flash.CMD_FLASH), true);
});

test('no ack is no ack, and a notification is not one', async () => {
  assert.equal(await flash.command(new FakeDuo(0x0050, { ack: false }), flash.CMD_ENTER_UPDATE, 100), false);
});

test('M goes only to a Duo in its normal mode, U only to the Duo in update mode', async () => {
  const h = hid(new FakeDuo(0x0093));
  assert.equal((await flash.chooseDuo(h)).productId, 0x0093);
  assert.ok(h.chooser[0].every((f) => f.vendorId === 0x19f7));
  await assert.rejects(flash.chooseDuo(hid(new FakeDuo(0x004f))), /update mode already/);
  await assert.rejects(flash.chooseDuo(hid(new FakeDuo(0x0037))), /not a RØDECaster Duo/); // a Pro II
  await assert.rejects(flash.chooseDuo(hid()), /no device chosen/);
  const u = hid(new FakeDuo(0x004f));
  assert.equal((await flash.chooseUpdateDuo(u)).productId, 0x004f);
  assert.deepEqual(u.chooser[0], [{ vendorId: 0x19f7, productId: 0x004f }]);
  await assert.rejects(flash.chooseUpdateDuo(hid(new FakeDuo(0x0050))), /not a RØDECaster Duo in update mode/);
});

test('only an empty disk is the update disk (what the system puts there aside)', async () => {
  await flash.checkDisk(new FakeDir('E:\\', { 'System Volume Information': '', '.fseventsd': '', '._x': '' }));
  await assert.rejects(flash.checkDisk(new FakeDir('USB-STICK', { 'photo.jpg': 'x', 'notes.txt': 'y' })), /not empty \(photo.jpg, notes.txt\)/);
});

test('the archive and its checksum arrive as they are, and are read back', async () => {
  const archive = new Blob([new Uint8Array(9 << 20).map((_, i) => i % 251)]); // more than two chunks
  const digest = createHash('md5').update(Buffer.from(await archive.arrayBuffer())).digest('hex');
  const disk = new FakeDir('NO NAME', { '.Trashes': '' });
  const steps = [];
  await flash.stage(disk, archive, digest, (done, total) => steps.push(done / total));
  const got = Buffer.from(await disk.files.get('archive.tar.gz').arrayBuffer());
  assert.equal(createHash('md5').update(got).digest('hex'), digest);
  assert.equal(await disk.files.get('archive.md5').text(), digest + '\n');
  assert.equal(steps.at(-1), 1);
  assert.equal(await flash.mounted(disk), true);
  disk.gone = true; // ejected
  assert.equal(await flash.mounted(disk), false);
});

test('an archive that does not match its checksum file is refused before anything is sent', () => {
  assert.equal(flash.checkPair('0123456789abcdef0123456789abcdef  archive.tar.gz\n', '0123456789abcdef0123456789abcdef'), '0123456789abcdef0123456789abcdef');
  assert.throws(() => flash.checkPair('0123456789abcdef0123456789abcdef', 'ffffffffffffffffffffffffffffffff'), /does not match/);
  assert.throws(() => flash.checkPair('', 'ffffffffffffffffffffffffffffffff'), /no checksum/);
});

test('the system decides how the disk is let go of', () => {
  assert.equal(flash.system({ userAgentData: { platform: 'Windows' } }), 'windows');
  assert.equal(flash.system({ platform: 'MacIntel' }), 'macos');
  assert.equal(flash.system({ platform: 'Linux x86_64' }), 'linux');
  assert.equal(flash.supported({ isSecureContext: true, navigator: { hid: {} }, showDirectoryPicker() {} }), true);
  assert.equal(flash.supported({ isSecureContext: true, navigator: {}, showDirectoryPicker() {} }), false);
});
