// An Ed25519 SSH key pair made in the browser (WebCrypto), as OpenSSH files:
// the private key in OpenSSH's own format (unencrypted, like ssh-keygen -N ''),
// the public key as one authorized_keys line. Nothing leaves the browser.

const enc = new TextEncoder();
const u32 = (n) => new Uint8Array([(n >>> 24) & 255, (n >>> 16) & 255, (n >>> 8) & 255, n & 255]);
const cat = (...parts) => {
  const out = new Uint8Array(parts.reduce((n, p) => n + p.length, 0));
  let o = 0;
  for (const p of parts) { out.set(p, o); o += p.length; }
  return out;
};
const str = (b) => cat(u32(b.length), typeof b === 'string' ? enc.encode(b) : b); // SSH "string"
const b64 = (b) => { let s = ''; for (const x of b) s += String.fromCharCode(x); return btoa(s); };
const unb64 = (s) => Uint8Array.from(atob(s), (c) => c.charCodeAt(0));
const unb64url = (s) => unb64(s.replace(/-/g, '+').replace(/_/g, '/') + '='.repeat((4 - (s.length % 4)) % 4));

// fingerprint is a public key line's SHA256 fingerprint as ssh-keygen -l
// shows it ("" for a line that is not a key).
export async function fingerprint(line) {
  const body = (line || '').trim().split(/\s+/)[1] || '';
  let wire;
  try { wire = unb64(body); } catch (e) { return ''; }
  if (!wire.length) return '';
  return 'SHA256:' + b64(new Uint8Array(await crypto.subtle.digest('SHA-256', wire))).replace(/=+$/, '');
}

export const supported = async () => {
  try { await crypto.subtle.generateKey({ name: 'Ed25519' }, true, ['sign', 'verify']); return true; } catch (e) { return false; }
};

// generate returns {publicKey, privateKey, fingerprint}: text for the two
// files and the key's SHA256 fingerprint as ssh-keygen -l shows it.
export async function generate(comment = 'rcplug') {
  const kp = await crypto.subtle.generateKey({ name: 'Ed25519' }, true, ['sign', 'verify']);
  const pub = new Uint8Array(await crypto.subtle.exportKey('raw', kp.publicKey));
  // The 32-byte seed is the JWK's "d" (RFC 8037), whatever layout a
  // browser's PKCS#8 export has.
  const jwk = await crypto.subtle.exportKey('jwk', kp.privateKey);
  const seed = unb64url(jwk.d || '');
  if (seed.length !== 32 || pub.length !== 32) throw new Error('unexpected Ed25519 key from this browser');
  const wirePub = cat(str('ssh-ed25519'), str(pub));
  const check = crypto.getRandomValues(new Uint8Array(4));
  let priv = cat(check, check, str('ssh-ed25519'), str(pub), str(cat(seed, pub)), str(comment));
  const pad = [];
  for (let i = 1; (priv.length + pad.length) % 8; i++) pad.push(i);
  priv = cat(priv, new Uint8Array(pad));
  const blob = cat(enc.encode('openssh-key-v1\0'), str('none'), str('none'), str(new Uint8Array(0)), u32(1), str(wirePub), str(priv));
  const lines = b64(blob).match(/.{1,70}/g).join('\n');

  return {
    publicKey: `ssh-ed25519 ${b64(wirePub)} ${comment}\n`,
    privateKey: `-----BEGIN OPENSSH PRIVATE KEY-----\n${lines}\n-----END OPENSSH PRIVATE KEY-----\n`,
    fingerprint: await fingerprint(`ssh-ed25519 ${b64(wirePub)}`),
  };
}
