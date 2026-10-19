// Flashing over USB from the browser: the toolkit's flash procedure
// (internal/flash/native.go, itself a port of rodecaster-duo-ssh's
// flash_cfw.py and rode_hid.py) with WebHID for the two commands and the File
// System Access API for the update disk. The same steps in the same order:
//
//  1. send 'M' as output report 1 and wait for the 'A' ack on input report 2
//  2. the device shows its update disk; the visitor picks it (it must be empty)
//  3. copy archive.tar.gz and archive.md5 onto it
//  4. let the data reach the device (the native flow dismounts the disk)
//  5. only after a typed confirmation: send 'U' the same way to the Duo in
//     update mode; it compares archive.tar.gz with archive.md5, flashes the
//     inactive partition and reboots
//
// The functions take the browser's objects as arguments, so the tests can
// hand in a simulated device and disk.

export const VENDOR = 0x19f7;
// The Duo's product ids in its normal modes (stereo, extend stereo, sync,
// multitrack, extend multitrack) and in update mode, from uac2_gadget.sh.
export const DUO_IDS = [0x0050, 0x0079, 0x008e, 0x0093, 0x0095];
export const UPDATE_ID = 0x004f;
const MODE_REPORT = 1, ACK_REPORT = 2, ACK = 0x41;
export const CMD_ENTER_UPDATE = 0x4d; // 'M'
export const CMD_FLASH = 0x55; // 'U'
export const ACK_TIMEOUT = 5000; // rode_hid.ACK_TIMEOUT_S
export const STAGED = 'archive.tar.gz', MD5 = 'archive.md5';
const CHUNK = 4 << 20;

export const supported = (g = globalThis) => !!(g.isSecureContext && g.navigator && g.navigator.hid && typeof g.showDirectoryPicker === 'function');

// The system the page runs on, for the steps that differ.
export function system(nav = globalThis.navigator) {
  const p = ((nav && nav.userAgentData && nav.userAgentData.platform) || (nav && (nav.platform || nav.userAgent)) || '').toLowerCase();
  return p.includes('win') ? 'windows' : p.includes('mac') ? 'macos' : p.includes('linux') || p.includes('x11') ? 'linux' : '';
}

const hex4 = (n) => n.toString(16).padStart(4, '0');
export const describe = (d) => `${d.productName || 'HID device'} (${hex4(d.vendorId)}:${hex4(d.productId)})`;

// chooseDuo asks the visitor for the Duo, in its normal mode: one in update
// mode already would get its update disk re-created under a run's feet.
export async function chooseDuo(hid) {
  const filters = [...DUO_IDS, UPDATE_ID].map((productId) => ({ vendorId: VENDOR, productId }));
  const [d] = await hid.requestDevice({ filters });
  if (!d) throw new Error('no device chosen');
  if (d.vendorId !== VENDOR) throw new Error(`${describe(d)} is not a RØDE device`);
  if (d.productId === UPDATE_ID) throw new Error(`${describe(d)} is in update mode already (an earlier run copied an archive onto it): switch the Duo off and on again, then start again`);
  if (!DUO_IDS.includes(d.productId)) throw new Error(`${describe(d)} is not a RØDECaster Duo`);
  return d;
}

// chooseUpdateDuo asks for the Duo in update mode, which is a new USB device
// to the browser (another product id): only that one ever gets 'U'.
export async function chooseUpdateDuo(hid) {
  const [d] = await hid.requestDevice({ filters: [{ vendorId: VENDOR, productId: UPDATE_ID }] });
  if (!d) throw new Error('no device chosen');
  if (d.vendorId !== VENDOR || d.productId !== UPDATE_ID) throw new Error(`${describe(d)} is not a RØDECaster Duo in update mode`);
  return d;
}

// reportLength is output report 1's payload length from the device's report
// descriptor (63 bytes on the Duo); the id is sent separately.
export function reportLength(d) {
  for (const c of d.collections || []) {
    for (const r of c.outputReports || []) {
      if (r.reportId !== MODE_REPORT) continue;
      const bits = (r.items || []).reduce((n, it) => n + (it.reportSize || 0) * (it.reportCount || 0), 0);
      if (bits >= 8) return Math.ceil(bits / 8);
    }
  }
  return 63;
}

// command sends a one-byte command as output report 1, zero-padded, and
// waits for the acknowledgement: input report 2 carrying 'A'. Other input
// reports (the device's notifications on report 4) are not an answer.
export async function command(d, cmd, timeout = ACK_TIMEOUT) {
  if (!d.opened) await d.open();
  let done;
  const ack = new Promise((resolve) => {
    const timer = setTimeout(() => finish(false), timeout);
    function on(e) { if (e.reportId === ACK_REPORT) finish(e.data.byteLength > 0 && e.data.getUint8(0) === ACK); }
    function finish(v) { clearTimeout(timer); d.removeEventListener('inputreport', on); resolve(v); }
    done = finish;
    d.addEventListener('inputreport', on);
  });
  const data = new Uint8Array(reportLength(d));
  data[0] = cmd;
  try {
    await d.sendReport(MODE_REPORT, data);
  } catch (e) {
    done(false);
    throw e;
  }
  return ack;
}

// What an operating system puts on a fresh FAT disk by itself.
const systemEntry = (name) => /^(System Volume Information|\$RECYCLE\.BIN|\.Trashes|\.fseventsd|\.Spotlight-V100|\.DS_Store|\.TemporaryItems|\._.*)$/i.test(name);

// checkDisk refuses a folder that is not an empty disk: the Duo formats its
// update disk anew on every 'M'.
export async function checkDisk(dir) {
  const other = [];
  for await (const name of dir.keys()) if (!systemEntry(name)) other.push(name);
  if (other.length) throw new Error(`“${dir.name}” is not empty (${other.slice(0, 5).join(', ')}${other.length > 5 ? ', …' : ''}): the Duo's update disk is, right after it appears. Choose the new, empty drive`);
}

async function writeFile(dir, name, blob, progress) {
  const fh = await dir.getFileHandle(name, { create: true });
  const w = await fh.createWritable();
  try {
    for (let o = 0; o < blob.size; o += CHUNK) {
      await w.write(blob.slice(o, Math.min(o + CHUNK, blob.size)));
      if (progress) progress(Math.min(o + CHUNK, blob.size), blob.size);
    }
    await w.close();
  } catch (e) {
    try { await w.abort(); } catch (x) { /* gone already */ }
    throw e;
  }
}

// stage copies the archive and then its checksum file to the disk's root, the
// names the device expects, and reads back what arrived.
export async function stage(dir, archive, md5Line, progress) {
  await checkDisk(dir);
  await writeFile(dir, STAGED, archive, progress);
  await writeFile(dir, MD5, new Blob([md5Line.trim() + '\n']));
  const a = await (await dir.getFileHandle(STAGED)).getFile();
  const m = await (await dir.getFileHandle(MD5)).getFile();
  if (a.size !== archive.size) throw new Error(`${STAGED} on the disk has ${a.size} bytes instead of ${archive.size}`);
  if ((await m.text()).trim() !== md5Line.trim()) throw new Error(`${MD5} on the disk does not say what was written`);
}

// mounted tells whether the disk is still there: once it is ejected the
// folder is gone and its files with it.
export async function mounted(dir) {
  try {
    await dir.getFileHandle(MD5);
    return true;
  } catch (e) {
    return false;
  }
}

// checkPair checks the archive against its checksum file the way the device
// will: the first word of archive.md5 must be the archive's MD5.
export function checkPair(md5Text, digest) {
  const declared = (md5Text || '').trim().split(/\s+/)[0].toLowerCase();
  if (!/^[0-9a-f]{32}$/.test(declared)) throw new Error(`${MD5} holds no checksum`);
  if (declared !== digest) throw new Error(`${STAGED} does not match ${MD5} (MD5 ${digest}, the file says ${declared}); the device would reject it — prepare the firmware again`);
  return declared;
}
