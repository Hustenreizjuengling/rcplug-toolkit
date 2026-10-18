#!/usr/bin/env bash
# End-to-end test of the wizard and the device installer without a device:
# a fake "ssh" runs the remote commands locally inside a sandbox directory.
# Covers install, update (data kept), --plugin and uninstall.
#
#   tools/installer-test.sh <payload dir> [rcplug-setup]   (make installer-test)
#
# The payload is the rcplug platform built for this machine (make package-host
# in the rcplug repository); RCPLUG_REPO names that repository (default
# ../rcplug next to this one), which provides the example plugins.
set -euo pipefail

PAYLOAD="$(cd "${1:?usage: $0 <payload dir> [rcplug-setup binary]}" && pwd)"
SETUP=""
[ -n "${2:-}" ] && SETUP="$(cd "$(dirname "$2")" && pwd)/$(basename "$2")"
REPO="$(cd "$(dirname "$0")/.." && pwd)"
RCPLUG_REPO="$(cd "${RCPLUG_REPO:-$REPO/../rcplug}" && pwd)"
SB="$(mktemp -d "${TMPDIR:-/tmp}/rcplug-installer-test.XXXXXX")"
PORT="${RCPLUG_TEST_PORT:-18580}"
cleanup() {
  if [ -x "$SB/initd/rcplug" ]; then RCPLUG_PIDFILE="$SB/rcplug.pid" "$SB/initd/rcplug" stop >/dev/null 2>&1 || true; fi
  rm -rf "$SB"
}
trap cleanup EXIT

command -v start-stop-daemon >/dev/null 2>&1 || { echo "SKIP: start-stop-daemon not available"; exit 0; }

mkdir -p "$SB/bin" "$SB/initd"
# The fake ssh drops the options and the destination and runs the command
# with the sandbox variables the installer and init script honour.
cat > "$SB/bin/ssh" <<FAKE
#!/bin/sh
while [ \$# -gt 0 ]; do
  case "\$1" in -o|-i) shift 2;; -*) shift;; *) shift; break;; esac
done
export RCPLUG_INITDIR="$SB/initd" RCPLUG_PIDFILE="$SB/rcplug.pid" RCPLUG_LOG="$SB/rcplug.log"
exec sh -c "\$*"
FAKE
chmod +x "$SB/bin/ssh"
export PATH="$SB/bin:$PATH"
wizard() { "$REPO/installer/rcplug-wizard.sh" --host sandbox --yes --addr "127.0.0.1:$PORT" "$@"; }
fail() { echo "FAIL: $*" >&2; [ -f "$SB/rcplug.log" ] && tail -20 "$SB/rcplug.log" >&2; exit 1; }
api() { curl -fsS --max-time 3 "http://127.0.0.1:$PORT$1"; }
wait_up() {
  for _ in $(seq 1 50); do api /api/system >/dev/null 2>&1 && return 0; sleep 0.2; done
  fail "dashboard did not come up"
}

echo "== install"
wizard --payload "$PAYLOAD" --dir "$SB/rcplug" >/dev/null
wait_up
[ -x "$SB/rcplug/rcplugd" ] || fail "rcplugd not installed"
if [ ! -d "$SB/rcplug/data" ] || [ ! -d "$SB/rcplug/config" ]; then fail "data/config directories missing"; fi
api /api/plugins | grep -q '"name":"console"' || fail "console plugin not listed"

echo "== update keeps data"
echo '{"keep":true}' > "$SB/rcplug/data/console/marker.json"
wizard --payload "$PAYLOAD" --dir "$SB/rcplug" >/dev/null
wait_up
[ -f "$SB/rcplug/data/console/marker.json" ] || fail "update removed plugin data"

echo "== update from a release tarball, reusing directory and address"
# Like a downloaded release: wizard, payload and examples tarball side by side,
# no --payload, --dir or --addr. v0.3 kept settings next to the plugin code.
mkdir -p "$SB/release"
cp "$REPO/installer/rcplug-wizard.sh" "$REPO/installer/rcplug-remote.sh" "$SB/release/"
tar -C "$PAYLOAD" -czf "$SB/release/rcplug-9.9.9-aarch64.tar.gz" .
tar -C "$RCPLUG_REPO/examples" -czf "$SB/release/rcplug-examples-9.9.9-aarch64.tar.gz" .
echo '{"readOnly":true}' > "$SB/rcplug/plugins/console/console.state.json"
"$SB/release/rcplug-wizard.sh" --host sandbox --yes >/dev/null
wait_up
[ -f "$SB/rcplug/data/console/console.state.json" ] || fail "old plugin state not moved to data/"
[ -f "$SB/rcplug/data/console/marker.json" ] || fail "data lost when reusing the install directory"

echo "== stale PID file"
RCPLUG_PIDFILE="$SB/rcplug.pid" RCPLUG_LOG="$SB/rcplug.log" "$SB/initd/rcplug" stop >/dev/null
sleep 300 &
victim=$!
echo "$victim" > "$SB/rcplug.pid"
RCPLUG_PIDFILE="$SB/rcplug.pid" RCPLUG_LOG="$SB/rcplug.log" "$SB/initd/rcplug" stop >/dev/null
kill -0 "$victim" 2>/dev/null || fail "stop killed an unrelated process from a stale PID file"
echo "$victim" > "$SB/rcplug.pid"
RCPLUG_PIDFILE="$SB/rcplug.pid" RCPLUG_LOG="$SB/rcplug.log" "$SB/initd/rcplug" start >/dev/null \
  || fail "start refused because of a stale PID file"
kill "$victim" 2>/dev/null || true
wait_up

echo "== host settings from config/rcplugd.env"
echo 'RCPLUG_ALLOW_HOST=studio.example' > "$SB/rcplug/config/rcplugd.env"
RCPLUG_PIDFILE="$SB/rcplug.pid" RCPLUG_LOG="$SB/rcplug.log" "$SB/initd/rcplug" restart >/dev/null
wait_up
curl -fsS --max-time 3 -H 'Host: studio.example' "http://127.0.0.1:$PORT/api/system" >/dev/null \
  || fail "RCPLUG_ALLOW_HOST from rcplugd.env not applied"

echo "== diagnostics from the running host"
for _ in $(seq 1 50); do
  [ "$(api /api/plugins | grep -o '"running":true' | wc -l)" -ge 1 ] && break
  sleep 0.2
done
zipnames() { python3 -c 'import sys,zipfile; print("\n".join(zipfile.ZipFile(sys.argv[1]).namelist()))' "$1"; }
wizard --diagnostics --out "$SB/diag.zip" >/dev/null
for f in README.txt rcplug.json problems.txt system/processes.txt logs/console.log plugins/console/status.json; do
  zipnames "$SB/diag.zip" | grep -qx "$f" || { python3 -c 'import sys,zipfile,json; d=json.loads(zipfile.ZipFile(sys.argv[1]).read("rcplug.json")); print([(p["name"],p["state"],p["hasUI"]) for p in d["plugins"]]); print(zipfile.ZipFile(sys.argv[1]).read("logs/console.log").decode()[-1500:])' "$SB/diag.zip" >&2; fail "diagnostics bundle lacks $f"; }
done

echo "== problems are reported on connect, diagnostics work with the host down"
echo "2026-01-01 00:00:00 panic: test crash" >> "$SB/rcplug/logs/console.log"
out="$(wizard --dry-run 2>&1 || true)"
grep -q "The device logged problems" <<<"$out" || { echo "$out" >&2; fail "logged problem not reported"; }
RCPLUG_PIDFILE="$SB/rcplug.pid" RCPLUG_LOG="$SB/rcplug.log" "$SB/initd/rcplug" stop >/dev/null
wizard --diagnostics --out "$SB/diag-offline.zip" >/dev/null
python3 -c 'import sys,zipfile; z=zipfile.ZipFile(sys.argv[1]); r=z.read("README.txt").decode(); p=z.read("problems.txt").decode(); sys.exit(0 if "did not answer" in r and "panic: test crash" in p else 1)' "$SB/diag-offline.zip" \
  || fail "offline bundle incomplete"
RCPLUG_PIDFILE="$SB/rcplug.pid" RCPLUG_LOG="$SB/rcplug.log" "$SB/initd/rcplug" start >/dev/null
wait_up

echo "== add a plugin"
wizard --plugin "$RCPLUG_REPO/examples/hello-sh" >/dev/null
[ -f "$SB/rcplug/plugins/hello-sh/plugin.json" ] || fail "plugin not copied"
for _ in $(seq 1 25); do api /api/plugins | grep -q '"name":"hello-sh"' && break; sleep 0.2; done
api /api/plugins | grep -q '"name":"hello-sh"' || fail "plugin not picked up by the rescan"

if [ -n "$SETUP" ]; then
  echo "== rcplug-setup: update, status, plugin, diagnostics"
  "$SETUP" --host sandbox --yes --payload "$SB/release/rcplug-9.9.9-aarch64.tar.gz" install >/dev/null || fail "rcplug-setup install failed"
  wait_up
  out="$("$SETUP" --host sandbox --yes --lang en status 2>&1)" || { echo "$out" >&2; fail "rcplug-setup status failed"; }
  grep -q "rcplug on the device: host: up" <<<"$out" || { echo "$out" >&2; fail "rcplug-setup status did not report the host"; }
  "$SETUP" --host sandbox --yes plugin "$RCPLUG_REPO/examples/hello-sh" >/dev/null || fail "rcplug-setup plugin failed"
  [ -x "$SB/rcplug/plugins/hello-sh/run.sh" ] || fail "rcplug-setup did not install the plugin with its modes"
  (cd "$SB" && "$SETUP" --host sandbox --yes diagnostics >/dev/null) || fail "rcplug-setup diagnostics failed"
  ls "$SB"/rcplug-diagnostics-sandbox-*.zip >/dev/null 2>&1 || fail "rcplug-setup saved no diagnostics"
fi

echo "== stop and start without a shell"
# As the setup runs it: the script on stdin over ssh.
ssh sandbox sh -s -- stop < "$REPO/installer/rcplug-remote.sh" | grep -q 'rcplug stopped' || fail "stop did not report"
if api /api/plugins >/dev/null 2>&1; then fail "rcplug still answers after stop"; fi
ssh sandbox sh -s -- start < "$REPO/installer/rcplug-remote.sh" | grep -q 'rcplug started' || fail "start did not report"
wait_up

echo "== dashboard password removed without a shell"
mkdir -p "$SB/rcplug/config"
echo '{"hash":"x","key":"y"}' > "$SB/rcplug/config/dashboard-access.json"
ssh sandbox sh -s -- resetaccess < "$REPO/installer/rcplug-remote.sh" | grep -q 'password removed' || fail "resetaccess did not report"
[ ! -e "$SB/rcplug/config/dashboard-access.json" ] || fail "the password file is still there"
ssh sandbox sh -s -- resetaccess < "$REPO/installer/rcplug-remote.sh" | grep -q 'has no password' || fail "a second resetaccess"

echo "== device identity (what the setup shows before an update)"
mkdir -p "$SB/sysroot/etc" "$SB/sysroot/usr/bin"
echo rodecaster-pro-ii-mini > "$SB/sysroot/etc/hostname"
echo 1.7.3 > "$SB/sysroot/usr/bin/rcp-os.version"
out="$(RCPLUG_SYSROOT="$SB/sysroot" sh "$REPO/installer/rcplug-remote.sh" device)"
[ "$out" = "$(printf 'hostname=rodecaster-pro-ii-mini\nfirmware=1.7.3')" ] || fail "device: $out"

echo "== uninstall"
wizard --uninstall >/dev/null
[ ! -e "$SB/rcplug" ] || fail "install directory still there"
[ ! -e "$SB/initd/rcplug" ] || fail "init script still there"
if api /api/system >/dev/null 2>&1; then fail "host still running"; fi
echo "OK"
