// The archives a diagnostics download comes in, read in the browser: rcplug's
// zip (stored or deflated) and the toolkit's fallback when rcplug does not
// answer, a .tar.gz of the raw logs. A bundle is a few MB, so everything is
// read into memory; limits keep a crafted file from taking more.

const MAX_TOTAL = 128 << 20; // all files unpacked
const MAX_FILES = 5000;

// unpack returns the archive's regular files: name → bytes.
export async function unpack(buf) {
  const u8 = new Uint8Array(buf);
  if (u8[0] === 0x50 && u8[1] === 0x4b) return unzip(u8);
  if (u8[0] === 0x1f && u8[1] === 0x8b) return untar(await inflate(u8, 'gzip', MAX_TOTAL));
  throw new Error('not a zip or tar.gz file');
}

// inflate decompresses with the browser's own DecompressionStream, stopping
// at limit bytes.
async function inflate(data, format, limit) {
  const reader = new Blob([data]).stream().pipeThrough(new DecompressionStream(format)).getReader();
  const parts = [];
  let n = 0;
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    n += value.length;
    if (n > limit) {
      reader.cancel();
      throw new Error('the archive unpacks to more than ' + (limit >> 20) + ' MB');
    }
    parts.push(value);
  }
  const out = new Uint8Array(n);
  let o = 0;
  for (const p of parts) { out.set(p, o); o += p.length; }
  return out;
}

const utf8 = new TextDecoder();

async function unzip(u8) {
  const dv = new DataView(u8.buffer, u8.byteOffset, u8.byteLength);
  let end = -1;
  for (let i = u8.length - 22; i >= Math.max(0, u8.length - 22 - 0xffff); i--) {
    if (dv.getUint32(i, true) === 0x06054b50) { end = i; break; }
  }
  if (end < 0) throw new Error('zip: no central directory (the file is cut off?)');
  const count = dv.getUint16(end + 10, true);
  if (count > MAX_FILES) throw new Error('zip: too many files');
  let p = dv.getUint32(end + 16, true);
  const out = new Map();
  let total = 0;
  for (let i = 0; i < count; i++) {
    if (p + 46 > u8.length || dv.getUint32(p, true) !== 0x02014b50) throw new Error('zip: damaged directory');
    const method = dv.getUint16(p + 10, true);
    const csize = dv.getUint32(p + 20, true);
    const usize = dv.getUint32(p + 24, true);
    const nlen = dv.getUint16(p + 28, true);
    const skip = nlen + dv.getUint16(p + 30, true) + dv.getUint16(p + 32, true);
    const local = dv.getUint32(p + 42, true);
    const name = utf8.decode(u8.subarray(p + 46, p + 46 + nlen));
    p += 46 + skip;
    if (name.endsWith('/')) continue;
    if (local + 30 > u8.length || dv.getUint32(local, true) !== 0x04034b50) throw new Error('zip: damaged entry ' + name);
    const start = local + 30 + dv.getUint16(local + 26, true) + dv.getUint16(local + 28, true);
    const data = u8.subarray(start, start + csize);
    if (data.length !== csize) throw new Error('zip: ' + name + ' is cut off');
    total += usize;
    if (total > MAX_TOTAL) throw new Error('the archive unpacks to more than ' + (MAX_TOTAL >> 20) + ' MB');
    if (method === 0) out.set(name, data);
    else if (method === 8) out.set(name, await inflate(data, 'deflate-raw', usize + 1));
    // other methods: never written by rcplug; left out
  }
  return out;
}

function cstr(b) {
  const z = b.indexOf(0);
  return utf8.decode(z < 0 ? b : b.subarray(0, z));
}

function untar(u8) {
  const out = new Map();
  let p = 0;
  let long = '';
  while (p + 512 <= u8.length && out.size < MAX_FILES) {
    const h = u8.subarray(p, p + 512);
    if (h.every((b) => b === 0)) break;
    let name = cstr(h.subarray(0, 100));
    const prefix = h[257] === 0x75 ? cstr(h.subarray(345, 500)) : ''; // "ustar"
    if (prefix) name = prefix + '/' + name;
    const size = parseInt(cstr(h.subarray(124, 136)).trim() || '0', 8);
    if (!Number.isFinite(size) || size < 0) throw new Error('tar: damaged header');
    const type = h[156];
    p += 512;
    const data = u8.subarray(p, p + size);
    p += Math.ceil(size / 512) * 512;
    if (type === 0x4c) { long = cstr(data); continue; } // GNU long name
    if (long) { name = long; long = ''; }
    if (type === 0 || type === 0x30 || type === 0x37) out.set(name.replace(/^\.\//, ''), data);
  }
  return out;
}

export const text = (bytes) => (bytes ? utf8.decode(bytes) : '');
