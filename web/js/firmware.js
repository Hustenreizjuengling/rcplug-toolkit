// The firmware page: RØDE's stock firmware from the visitor's computer, an
// SSH key made here or pasted, and the toolkit's own patcher (internal/cfw,
// compiled to WebAssembly) adding the key. Nothing leaves the browser.
import { generate, supported, fingerprint } from './sshkey.js';
import { t, applyTexts, setupLang } from './i18n.js';

const $ = (s) => document.querySelector(s);
const state = {
  file: null, key: '', releases: [], wasm: null, running: false,
  gen: null, genSaved: true, // a key made here, and whether its private half was saved
  privUrl: '', pubUrl: '', outUrl: '', md5Url: '',
};

setupLang();
applyTexts();

// ---- the patcher (rcplug-web.wasm with Go's wasm_exec.js); both carry the
// build's version, so a cached pair never mixes two builds.
let build = '';
const buildInfo = fetch('toolkit.json', { cache: 'no-cache' }).then((r) => r.json()).then((d) => { build = d.wasm || d.version || ''; }).catch(() => {});

function loadPatcher() {
  if (state.wasm) return state.wasm;
  state.wasm = (async () => {
    await buildInfo;
    const v = build ? '?v=' + encodeURIComponent(build) : '';
    await new Promise((res, rej) => {
      const s = document.createElement('script');
      s.src = 'wasm_exec.js' + v;
      s.onload = res;
      s.onerror = () => rej(new Error('wasm_exec.js'));
      document.head.append(s);
    });
    const go = new globalThis.Go();
    const { instance } = await WebAssembly.instantiateStreaming(fetch('rcplug-web.wasm' + v), go.importObject);
    // The patcher ending (it should not) fails a running prepare and loads
    // it again next time.
    const exited = go.run(instance).then(() => { throw new Error(t('patcherStopped')); });
    exited.catch(() => { state.wasm = null; });
    return { api: globalThis.rcplugWeb, exited };
  })();
  state.wasm.catch(() => { state.wasm = null; }); // a failed load is tried again
  return state.wasm;
}

// ---- whatever changes, an earlier result no longer fits
function invalidate() {
  for (const k of ['outUrl', 'md5Url']) if (state[k]) { URL.revokeObjectURL(state[k]); state[k] = ''; }
  $('#result').replaceChildren();
  $('#prog').hidden = true;
  update();
}

function busy(on) {
  state.running = on;
  for (const el of document.querySelectorAll('#file, #gen, #pub, #keyMode button')) el.disabled = on || (el.dataset.mode === 'new' && !state.canGen);
  $('#drop').classList.toggle('off', on);
  update();
}

// ---- step 1: the firmware
async function loadReleases() {
  const box = $('#releases');
  try {
    const d = await (await fetch('firmware.json', { cache: 'no-cache' })).json();
    state.releases = d.releases || [];
    box.replaceChildren(...state.releases.map((r) => {
      const row = document.createElement('div');
      row.className = 'release';
      row.innerHTML = '<div><b></b> <span class="tag"></span><div class="dim small mono"></div></div><a class="btn sm" target="_blank" rel="noopener"></a>';
      row.querySelector('b').textContent = 'RØDECaster Duo ' + r.version;
      row.querySelector('.tag').textContent = t(r.channel === 'beta' ? 'beta' : 'stable');
      row.querySelector('.mono').textContent = 'MD5 ' + r.md5;
      const a = row.querySelector('a');
      a.href = r.url;
      a.textContent = t('download');
      return row;
    }));
    if (d.checked) box.append(Object.assign(document.createElement('p'), { className: 'hint', textContent: t('listedAt', { at: new Date(d.checked).toLocaleDateString() }) }));
  } catch (e) {
    box.replaceChildren(Object.assign(document.createElement('p'), { className: 'hint', textContent: t('noList') }));
  }
}

function setFile(f) {
  if (!f || state.running) return;
  state.file = f;
  $('#dropText').textContent = t('chosen', { name: f.name, mb: (f.size / 1048576).toFixed(0) });
  $('#drop').classList.add('ok');
  invalidate();
}
$('#file').addEventListener('change', (e) => setFile(e.target.files[0]));
const drop = $('#drop');
drop.addEventListener('dragover', (e) => { e.preventDefault(); if (!state.running) drop.classList.add('over'); });
drop.addEventListener('dragleave', () => drop.classList.remove('over'));
drop.addEventListener('drop', (e) => { e.preventDefault(); drop.classList.remove('over'); setFile(e.dataTransfer.files[0]); });

// ---- step 2: the key
let mode = 'new';
document.querySelectorAll('#keyMode button').forEach((b) => b.addEventListener('click', () => {
  if (state.running || mode === b.dataset.mode) return;
  mode = b.dataset.mode;
  document.querySelectorAll('#keyMode button').forEach((x) => x.setAttribute('aria-checked', String(x === b)));
  $('#keyNewBox').hidden = mode !== 'new';
  $('#keyOwnBox').hidden = mode !== 'own';
  invalidate();
  if (mode === 'new') { state.key = state.gen ? state.gen.publicKey.trim() : ''; update(); } else checkOwn();
}));

$('#gen').addEventListener('click', async () => {
  if (state.gen && !state.genSaved && !confirm(t('genReplace'))) return;
  const k = await generate('rodecaster');
  state.gen = k;
  state.genSaved = false;
  state.key = k.publicKey.trim();
  for (const u of [state.privUrl, state.pubUrl]) if (u) URL.revokeObjectURL(u);
  state.privUrl = URL.createObjectURL(new Blob([k.privateKey], { type: 'application/octet-stream' }));
  state.pubUrl = URL.createObjectURL(new Blob([k.publicKey], { type: 'text/plain' }));
  $('#privDl').href = state.privUrl;
  $('#pubDl').href = state.pubUrl;
  $('#keyFiles').hidden = false;
  $('#fp').textContent = k.fingerprint;
  $('#gen span').textContent = t('genAgain');
  invalidate();
});
$('#privDl').addEventListener('click', () => { state.genSaved = true; });
// A key made here that was not saved would be lost with the page.
window.addEventListener('beforeunload', (e) => {
  if (state.gen && !state.genSaved) { e.preventDefault(); e.returnValue = ''; }
});

let ownSeq = 0;
async function checkOwn() {
  const v = $('#pub').value.trim();
  const seq = ++ownSeq;
  state.key = '';
  invalidate();
  if (!v) { $('#pubState').textContent = t('pubHint'); $('#pubState').className = 'hint'; return; }
  let err;
  try { err = (await loadPatcher()).api.checkKey(v); } catch (e) { err = e.message; }
  if (seq !== ownSeq) return; // typed on meanwhile
  $('#pubState').textContent = err ? t('pubBad', { err }) : t('pubOk');
  $('#pubState').className = err ? 'error' : 'hint ok-text';
  if (!err) state.key = v;
  update();
}
$('#pub').addEventListener('input', checkOwn);

// ---- step 3: prepare
function update() { $('#go').disabled = state.running || !state.file || !state.key || !state.canRun; }

// askUnknown: a file that is none of RØDE's current releases is only used
// after the visitor says so.
function askUnknown(info) {
  return new Promise((resolve) => {
    const box = document.createElement('div');
    box.className = 'banner warn';
    box.innerHTML = '<svg class="i" aria-hidden="true"><use href="ui/icons.svg#alert"/></svg><div><p></p><div class="cluster"><button type="button" class="btn sm"></button><button type="button" class="btn sm ghost"></button></div></div>';
    box.querySelector('p').textContent = t('unknownAsk', { v: info.version || '?', md5: info.md5 });
    const [yes, no] = box.querySelectorAll('button');
    yes.textContent = t('unknownGo');
    no.textContent = t('unknownStop');
    yes.onclick = () => { box.remove(); resolve(true); };
    no.onclick = () => { box.remove(); resolve(false); };
    $('#result').replaceChildren(box);
    no.focus();
  });
}

$('#go').addEventListener('click', async () => {
  invalidate();
  busy(true);
  $('#prog').hidden = false;
  $('#progBar').style.width = '0%';
  const chunks = [];
  const t0 = Date.now();
  let known = null;
  const key = state.key;
  try {
    const w = await loadPatcher();
    const run = w.api.prepare({
      file: state.file, keys: [key],
      onChunk: (u8) => chunks.push(u8),
      onProgress: (phase, done, total) => {
        const p = Math.round((100 * done) / total);
        const all = phase === 'check' ? p / 3 : 33 + (2 * p) / 3; // the rewrite takes about twice as long
        $('#progText').textContent = t(phase === 'check' ? 'checking' : 'writing');
        $('#progPct').textContent = Math.round(all) + ' %';
        $('#progBar').style.width = all.toFixed(1) + '%';
      },
      onChecked: (info) => {
        known = state.releases.find((x) => x.md5 === info.md5) || null;
        return known ? true : askUnknown(info);
      },
    });
    const r = await Promise.race([run, w.exited]);
    const out = new Blob(chunks, { type: 'application/gzip' });
    chunks.length = 0;
    state.outUrl = URL.createObjectURL(out);
    state.md5Url = URL.createObjectURL(new Blob([r.md5Line + '\n'], { type: 'text/plain' }));
    const res = document.createElement('div');
    res.className = 'stack';
    res.innerHTML = `<div class="banner ok"><svg class="i" aria-hidden="true"><use href="ui/icons.svg#check"/></svg><div></div></div>
      <dl class="kv"></dl>
      <div class="cluster"><a class="btn primary" download="archive.tar.gz"><svg class="i i-sm" aria-hidden="true"><use href="ui/icons.svg#download"/></svg>archive.tar.gz</a>
        <a class="btn" download="archive.md5"><svg class="i i-sm" aria-hidden="true"><use href="ui/icons.svg#download"/></svg>archive.md5</a></div>`;
    res.querySelector('.banner div').textContent = t('done', { s: Math.round((Date.now() - t0) / 1000) });
    const kv = [[t('kModel'), (r.model || '?') + (r.version ? ' ' + r.version : '')],
      [t('kInput'), known ? t('inputKnown', { v: known.version }) : t('inputUnknown')],
      [t('kKey'), (await fingerprint(key)) || '?'], [t('kOut'), r.digest]];
    res.querySelector('dl').replaceChildren(...kv.flatMap(([k, v]) => [Object.assign(document.createElement('dt'), { textContent: k }), Object.assign(document.createElement('dd'), { textContent: v })]));
    const links = res.querySelectorAll('a');
    links[0].href = state.outUrl;
    links[1].href = state.md5Url;
    $('#result').replaceChildren(res);
    $('#progText').textContent = t('ready');
  } catch (e) {
    chunks.length = 0;
    $('#prog').hidden = true;
    if (e.message !== 'stopped') $('#result').replaceChildren(Object.assign(document.createElement('div'), { className: 'banner danger', textContent: t('failed', { err: e.message }) }));
    else $('#result').replaceChildren();
  } finally {
    busy(false);
  }
});

(async () => {
  state.canRun = !!globalThis.WebAssembly;
  if (!state.canRun) $('#unsupported').hidden = false;
  state.canGen = await supported();
  if (!state.canGen) { // no Ed25519 in this browser's WebCrypto: paste a key
    document.querySelector('#keyMode [data-mode="own"]').click();
    document.querySelector('#keyMode [data-mode="new"]').disabled = true;
  }
  update();
  loadReleases();
})();
