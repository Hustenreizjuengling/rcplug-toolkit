// The keys the firmware page makes are what OpenSSH makes: ssh-keygen reads
// the private key, derives the same public key and shows the same fingerprint.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { mkdtempSync, writeFileSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { generate, supported, fingerprint } from '../../web/js/sshkey.js';

test('an Ed25519 key pair OpenSSH accepts', async (t) => {
  assert.ok(await supported(), 'WebCrypto Ed25519');
  const dir = mkdtempSync(join(tmpdir(), 'sshkey-'));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  for (let i = 0; i < 5; i++) {
    const k = await generate('rcplug-test');
    const priv = join(dir, 'k' + i);
    writeFileSync(priv, k.privateKey, { mode: 0o600 });
    writeFileSync(priv + '.pub', k.publicKey);
    assert.match(k.publicKey, /^ssh-ed25519 [A-Za-z0-9+/]+=* rcplug-test\n$/);
    const derived = execFileSync('ssh-keygen', ['-y', '-f', priv], { encoding: 'utf8' }).trim();
    assert.equal(derived.split(' ').slice(0, 2).join(' '), k.publicKey.split(' ').slice(0, 2).join(' '));
    const fp = execFileSync('ssh-keygen', ['-l', '-f', priv + '.pub'], { encoding: 'utf8' }).split(' ')[1];
    assert.equal(fp, k.fingerprint);
  }
});

test('the fingerprint of a pasted key is the one ssh-keygen shows', async (t) => {
  const dir = mkdtempSync(join(tmpdir(), 'sshkey-'));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  for (const type of ['ed25519', 'rsa']) {
    const k = join(dir, type);
    execFileSync('ssh-keygen', ['-q', '-t', type, '-N', '', '-C', 'pasted', '-f', k]);
    const line = readFileSync(k + '.pub', 'utf8');
    const want = execFileSync('ssh-keygen', ['-l', '-f', k + '.pub'], { encoding: 'utf8' }).split(' ')[1];
    assert.equal(await fingerprint(line), want, type);
  }
  assert.equal(await fingerprint('not a key'), '');
});
