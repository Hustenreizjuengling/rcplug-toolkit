// Step 4 of the firmware page: flashing over USB from the browser (flash.js),
// with the archive from step 3 or with archive.tar.gz and archive.md5 the
// visitor chooses. Each step is logged the way rcplug-flash logs it, so a
// run can be followed and shared.
import * as flash from './flash.js';
import { t } from './i18n.js';

const $ = (s) => document.querySelector(s);
const st = { archive: null, dev: null, dir: null, step: 'a', sys: flash.system(), busy: false };
let patcher = null;

function log(line) {
  const el = $('#fLog');
  el.hidden = false;
  el.textContent += new Date().toLocaleTimeString() + '  ' + line + '\n';
  el.scrollTop = el.scrollHeight;
}

// go marks the step the visitor is at; earlier ones are done, later ones wait.
function go(step) {
  st.step = step;
  const order = ['a', 'b', 'c', 'd', 'e', 'f'];
  for (const s of order.slice(0, 5)) {
    const li = $('#f' + s.toUpperCase());
    li.classList.toggle('done', order.indexOf(s) < order.indexOf(step));
    li.classList.toggle('active', s === step);
  }
  update();
}

function update() {
  const b = st.busy;
  $('#fFiles').disabled = b || st.step !== 'a' && st.step !== 'b';
  $('#fM').disabled = b || st.step !== 'b' || !st.archive;
  $('#fDisk').disabled = b || st.step !== 'c';
  $('#fLet').disabled = b || st.step !== 'd';
  $('#fU').disabled = b || st.step !== 'e' || $('#fWord').value.trim().toLowerCase() !== 'flash';
}

function fail(where, e) {
  st.busy = false;
  log('✗ ' + e.message);
  $(where).replaceChildren(Object.assign(document.createElement('div'), { className: 'banner danger', textContent: t('failed', { err: e.message }) }));
  update();
}
const clear = (where) => $(where).replaceChildren();

// ---- a: what is flashed
function showArchive(a, own) {
  const rows = [[t('kModel'), (a.modelName || 'RØDECaster Duo') + (a.version ? ' ' + a.version : '')],
    [t('fKeys'), a.keyFps && a.keyFps.length ? a.keyFps.join(', ') : t('fNoKey')], ['MD5', a.digest]];
  const dl = document.createElement('dl');
  dl.className = 'kv';
  for (const [k, v] of rows) dl.append(Object.assign(document.createElement('dt'), { textContent: k }), Object.assign(document.createElement('dd'), { textContent: v }));
  const out = [Object.assign(document.createElement('p'), { className: 'hint', textContent: own ? t('fFromStep3') : t('fFromFiles') }), dl];
  if (!a.keyFps || !a.keyFps.length) out.push(Object.assign(document.createElement('div'), { className: 'banner warn', textContent: t('fNoKeyWarn') }));
  $('#fSrcInfo').replaceChildren(...out);
}

// setArchive takes the archive step 3 made (or null when it no longer fits).
export function setArchive(a) {
  if (st.step !== 'a' && st.step !== 'b') return; // a run is under way: it keeps its archive
  st.archive = a;
  if (a) { showArchive(a, true); log(`archive from step 3: ${a.modelName || ''} ${a.version || ''}, md5 ${a.digest}`); go('b'); } else { clear('#fSrcInfo'); go('a'); }
}

async function chooseFiles(list) {
  const files = [...list];
  const archive = files.find((f) => /\.gz$/i.test(f.name));
  const md5 = files.find((f) => /\.md5$/i.test(f.name));
  clear('#fSrcErr');
  if (!archive || !md5) { fail('#fSrcErr', new Error(t('fPickBoth'))); return; }
  st.busy = true;
  st.archive = null;
  update();
  $('#fSrcInfo').replaceChildren(Object.assign(document.createElement('p'), { className: 'hint', textContent: t('fChecking', { name: archive.name }) }));
  try {
    const p = await patcher();
    const info = await p.api.inspect({ file: archive, onProgress: (done, total) => { $('#fSrcInfo').firstChild.textContent = t('fChecking', { name: archive.name }) + ' ' + Math.round((100 * done) / total) + ' %'; } });
    const md5Text = await md5.text();
    flash.checkPair(md5Text, info.md5);
    if (info.model !== 'duo') throw new Error(t('fNotDuo', { name: archive.name }));
    st.archive = { blob: archive, md5Line: md5Text.trim(), digest: info.md5, modelName: info.modelName, version: info.version, keyFps: info.keyFingerprints || [] };
    showArchive(st.archive, false);
    log(`archive ${archive.name} (${Math.round(archive.size / 1048576)} MB, md5 ${info.md5}, matches ${md5.name}): ${info.modelName} ${info.version}, ${info.keys} SSH key(s)`);
    st.busy = false;
    go('b');
  } catch (e) {
    clear('#fSrcInfo');
    fail('#fSrcErr', e);
  }
}

// ---- b: update mode ('M')
async function enterUpdate() {
  clear('#fMErr');
  st.busy = true;
  update();
  try {
    const d = await flash.chooseDuo(navigator.hid); // first: the click's user activation
    log('device: ' + flash.describe(d));
    log("sending 'M' (enter update mode)...");
    const acked = await flash.command(d, flash.CMD_ENTER_UPDATE);
    try { await d.close(); } catch (e) { /* it leaves the bus for update mode anyway */ }
    if (!acked) throw new Error(t('fNoAckM'));
    log("'M' acknowledged; the Duo shows its update disk in a moment");
    st.busy = false;
    go('c');
  } catch (e) {
    fail('#fMErr', e);
  }
}

// ---- c: the update disk
// What an operating system calls the Duo's unlabelled FAT disk.
const diskName = { windows: /^[A-Z]:\\?$/i, macos: /^(NO NAME|UNTITLED)$/i, linux: /^[0-9A-F]{4}-[0-9A-F]{4}$/i };

async function copyToDisk() {
  clear('#fDiskErr');
  st.busy = true;
  update();
  try {
    const dir = await window.showDirectoryPicker({ id: 'rodecaster-update', mode: 'readwrite' });
    log(`update disk: “${dir.name}”`);
    const want = diskName[st.sys];
    if (want && !want.test(dir.name) && !confirm(t('fOddDisk', { name: dir.name }))) throw new Error(t('fNotUsed', { name: dir.name }));
    const bar = $('#fBar');
    $('#fProg').hidden = false;
    log(`copying ${flash.STAGED} onto “${dir.name}”...`);
    let next = 10;
    await flash.stage(dir, st.archive.blob, st.archive.md5Line, (done, total) => {
      const p = (100 * done) / total;
      bar.style.width = p.toFixed(1) + '%';
      $('#fPct').textContent = Math.round(p) + ' %';
      for (; next <= 100 && p >= next; next += 10) log(`  ${String(next).padStart(3)}%  ${done >> 20} of ${total >> 20} MB`);
    });
    log(`copied ${flash.STAGED} and ${flash.MD5}; read back: sizes and checksum file as written`);
    st.dir = dir;
    st.busy = false;
    go('d');
    letGo();
  } catch (e) {
    fail('#fDiskErr', e.name === 'AbortError' ? new Error(t('fNoDisk')) : e);
  }
}

// ---- d: the data has to reach the device before 'U'
function letGo() {
  const name = st.dir ? st.dir.name : '';
  $('#fLetText').textContent = t('fLet_' + (st.sys || 'linux'), { name });
  $('#fLet').textContent = t(st.sys === 'windows' ? 'fLetWait' : 'fLetDone');
  if (st.sys === 'windows') { // written through on Windows; give it a moment, no eject (the Duo must stay connected)
    let n = 10;
    $('#fLet').disabled = true;
    const tick = () => {
      $('#fLet').textContent = t('fLetWaitN', { n });
      if (n-- > 0) { setTimeout(tick, 1000); return; }
      $('#fLet').textContent = t('fLetDone');
      log('waited 10 s for the copy to settle');
      go('e');
    };
    tick();
  }
}

async function confirmLetGo() {
  if (st.sys === 'macos' && (await flash.mounted(st.dir))) {
    $('#fLetErr').replaceChildren(Object.assign(document.createElement('div'), { className: 'banner warn', textContent: t('fStillMounted', { name: st.dir.name }) }));
    return;
  }
  clear('#fLetErr');
  log(st.sys === 'macos' ? `“${st.dir.name}” ejected` : 'disk let go of (unmounted or synced)');
  go('e');
}

// ---- e: flash ('U'), only after the typed confirmation
async function flashNow() {
  clear('#fUErr');
  st.busy = true;
  update();
  try {
    const d = await flash.chooseUpdateDuo(navigator.hid); // first: the click's user activation
    log('device in update mode: ' + flash.describe(d));
    log("sending 'U' (flash + reboot)...");
    const acked = await flash.command(d, flash.CMD_FLASH);
    log('flash command ack=' + (acked ? 'yes' : 'no'));
    if (!acked) throw new Error(t('fNoAckU'));
    try { await d.close(); } catch (e) { /* rebooting */ }
    st.busy = false;
    go('f');
    $('#fUErr').replaceChildren(Object.assign(document.createElement('div'), { className: 'banner ok', textContent: t('fFlashing') }));
  } catch (e) {
    fail('#fUErr', e);
  }
}

// initFlash wires step 4; getPatcher loads the WebAssembly patcher.
export function initFlash(getPatcher) {
  patcher = getPatcher;
  if (!flash.supported()) { $('#fNo').hidden = false; return; }
  $('#flashBox').hidden = false;
  $('#fFiles').addEventListener('change', (e) => chooseFiles(e.target.files));
  $('#fM').addEventListener('click', enterUpdate);
  $('#fDisk').addEventListener('click', copyToDisk);
  $('#fLet').addEventListener('click', confirmLetGo);
  $('#fWord').addEventListener('input', update);
  $('#fU').addEventListener('click', flashNow);
  $('#fCopyLog').addEventListener('click', () => navigator.clipboard.writeText($('#fLog').textContent).then(() => { $('#fCopyLog').textContent = t('fCopied'); }, () => {}));
  if (st.sys === 'linux') $('#fLinux').hidden = false;
  go('a');
}
