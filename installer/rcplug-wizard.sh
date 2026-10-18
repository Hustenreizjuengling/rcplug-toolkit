#!/usr/bin/env bash
# rcplug install wizard: pushes the plugin host and its plugins to a
# RØDECaster over SSH and starts it. Run it on your computer (Linux, macOS,
# or Windows with Git Bash/WSL). Nothing on the device changes before you
# confirm.
#
#   ./rcplug-wizard.sh --host 192.168.1.50                 install or update
#   ./rcplug-wizard.sh --host 192.168.1.50 --plugin ./reclog   add one plugin
#   ./rcplug-wizard.sh --host 192.168.1.50 --diagnostics   download logs
#   ./rcplug-wizard.sh --host 192.168.1.50 --uninstall     remove everything
#
# Options:
#   --host H        device IP or hostname (asked for if missing)
#   --user U        SSH user (default root)
#   --key FILE      SSH private key
#   --ask-password  prompt for the SSH password (needs sshpass); the variable
#                   RCPLUG_SSH_PASSWORD works too. Never put it on the command line.
#                   Only for devices that accept a password: the Duo does not.
#   --addr A        dashboard listen address on the device (default :8080, or
#                   the address of the existing installation)
#   --dir D         install directory on the device (default: the existing
#                   installation, else /usr/local/rcplug)
#   --payload P     staged payload directory or rcplug-*-aarch64.tar.gz
#   --plugin DIR    install or update one plugin directory (repeatable) into an
#                   existing installation
#   --diagnostics   download a diagnostics bundle (logs, versions, device state)
#                   to the current folder, or to the file given with --out FILE.
#                   Works even when the web interface does not respond.
#   --uninstall     stop rcplug and remove it, including plugin data
#   --yes           do not ask for confirmation
#   --dry-run       only check the connection and show what would happen
set -euo pipefail

HOST=""; USER_NAME="root"; ADDR=""; KEY=""; DIR_OVERRIDE=""
UNINSTALL=0; DRYRUN=0; YES=0; ASK_PASSWORD=0; DIAG=0; OUT=""
PLUGINS=()
HERE="$(cd "$(dirname "$0")" && pwd)"
PAYLOAD=""

usage() { sed -n '2,/^set -euo/p' "$0" | sed '$d; s/^# \{0,1\}//'; exit "${1:-0}"; }
die() { echo "error: $1" >&2; exit "${2:-1}"; }
need_arg() { if [ $# -lt 2 ] || [ -z "$2" ]; then die "$1 needs a value" 2; fi; }

while [ $# -gt 0 ]; do
  case "$1" in
    --host) need_arg "$@"; HOST="$2"; shift 2;;
    --user) need_arg "$@"; USER_NAME="$2"; shift 2;;
    --key) need_arg "$@"; KEY="$2"; shift 2;;
    --ask-password) ASK_PASSWORD=1; shift;;
    --addr) need_arg "$@"; ADDR="$2"; shift 2;;
    --dir) need_arg "$@"; DIR_OVERRIDE="$2"; shift 2;;
    --payload) need_arg "$@"; PAYLOAD="$2"; shift 2;;
    --plugin) need_arg "$@"; PLUGINS+=("$2"); shift 2;;
    --diagnostics) DIAG=1; shift;;
    --out) need_arg "$@"; OUT="$2"; shift 2;;
    --uninstall) UNINSTALL=1; shift;;
    --yes|-y) YES=1; shift;;
    --dry-run) DRYRUN=1; shift;;
    --password) die "--password was removed: it leaks into the process list. Use --ask-password or RCPLUG_SSH_PASSWORD." 2;;
    -h|--help) usage 0;;
    *) echo "unknown option: $1" >&2; usage 2;;
  esac
done

# Both values end up in the device's init script: plain values only.
if [ -n "$ADDR" ] && ! [[ "$ADDR" =~ ^([0-9]{1,3}(\.[0-9]{1,3}){3})?:[0-9]{1,5}$ ]]; then
  die "--addr must look like :8080 or 192.168.1.50:8080" 2
fi
if [ -n "$DIR_OVERRIDE" ] && ! [[ "$DIR_OVERRIDE" =~ ^/[A-Za-z0-9._/-]+$ ]]; then
  die "--dir must be an absolute path of letters, digits, '.', '_', '-' and '/'" 2
fi
# Plugin directories are checked before anything else happens. (The
# ${a[@]+...} form keeps empty arrays working with set -u in bash 3.2 on macOS.)
for p in ${PLUGINS[@]+"${PLUGINS[@]}"}; do
  [ -f "$p/plugin.json" ] || die "$p has no plugin.json" 5
  name="$(basename "$(cd "$p" && pwd)")"
  [[ "$name" =~ ^[a-z0-9][a-z0-9_-]*$ ]] || die "plugin directory name '$name' must use a-z, 0-9, - and _" 2
done
# The device side of every step is rcplug-remote.sh, sent along with each
# call, so nothing has to be on the device beforehand.
REMOTE_SH="$HERE/rcplug-remote.sh"
[ -f "$REMOTE_SH" ] || die "rcplug-remote.sh is missing next to the wizard" 5
if [ -z "$HOST" ]; then
  printf "RØDECaster IP or hostname: "
  read -r HOST || die "no host given" 2
fi
[ -n "$HOST" ] || die "no host given" 2

# sq quotes a value for the device's POSIX shell.
sq() { printf "'%s'" "$(printf '%s' "$1" | sed "s/'/'\\\\''/g")"; }

# SSH: the host key is remembered on first use and verified afterwards. A
# firmware update may give the device a new key; remove the old one with
# "ssh-keygen -R <host>" then. ssh-rsa stays allowed for older devices.
SSH_OPTS=(-o StrictHostKeyChecking=accept-new -o ConnectTimeout=8 -o LogLevel=ERROR
          -o PubkeyAcceptedKeyTypes=+ssh-rsa -o HostKeyAlgorithms=+ssh-rsa)
[ -n "$KEY" ] && SSH_OPTS+=(-i "$KEY")
if [ $ASK_PASSWORD = 1 ] && [ -z "${RCPLUG_SSH_PASSWORD:-}" ]; then
  printf "SSH password for %s@%s: " "$USER_NAME" "$HOST"
  read -rs RCPLUG_SSH_PASSWORD; echo
fi
PASS="${RCPLUG_SSH_PASSWORD:-}"
if [ -n "$PASS" ]; then
  command -v sshpass >/dev/null 2>&1 || die "sshpass is needed for password logins (or set up an SSH key)" 3
fi

# remote runs a command on the device; stdin is passed through. The password
# goes to sshpass only, through its environment.
remote() {
  if [ -n "$PASS" ]; then
    SSHPASS="$PASS" sshpass -e ssh "${SSH_OPTS[@]}" "$USER_NAME@$HOST" "$@"
  else
    # shellcheck disable=SC2029 # the command is meant to run remotely as built
    ssh "${SSH_OPTS[@]}" "$USER_NAME@$HOST" "$@"
  fi
}
# device runs a command of rcplug-remote.sh on the device.
device() { remote sh -s -- "$@" < "$REMOTE_SH"; }
# upload copies stdin to a file on the device.
upload() { remote "cat > $(sq "$1")"; }

# pack writes a gzipped tar of a folder to stdout, owned by root as on the
# device (the device's tar, run as root, restores the owners it finds).
pack() {
  if tar --version 2>/dev/null | grep -q GNU; then
    tar --owner=0 --group=0 --numeric-owner -C "$1" -czf - .
  else
    tar --uid 0 --gid 0 -C "$1" -czf - . # bsdtar (macOS)
  fi
}

confirm() {
  [ $YES = 1 ] && return 0
  printf "%s [y/N] " "$1"
  read -r ans
  case "$ans" in y|Y|yes|j|J|ja) return 0;; esac
  echo "aborted."; exit 0
}

action=install
[ ${#PLUGINS[@]} -gt 0 ] && action="install plugin(s): ${PLUGINS[*]}"
[ $UNINSTALL = 1 ] && action=uninstall
[ $DIAG = 1 ] && action="download diagnostics"
echo "== rcplug wizard =="
echo "   device : $USER_NAME@$HOST"
echo "   action : $action"

echo "-- checking SSH"
# Capture first: piping into "grep -q" under pipefail can fail spuriously.
probe_err="$(mktemp)"
probe="$(remote 'echo ok; uname -m' 2>"$probe_err" || true)"
if grep -q 'REMOTE HOST IDENTIFICATION HAS CHANGED\|Host key verification failed' "$probe_err"; then
  rm -f "$probe_err"
  die "the device's SSH host key changed (a firmware update can do that). If you trust it: ssh-keygen -R $HOST" 4
fi
rm -f "$probe_err"
if [ "${probe%%$'\n'*}" != "ok" ]; then
  cat >&2 <<'EOF'
Could not log in over SSH.
  * Check the IP address (on the device: Settings -> System -> Network).
  * The computer and the device must be on the same network.
  * Log in with your SSH key (--key); the Duo accepts no password.
  * After a firmware update the host key has changed (ssh-keygen -R <host>), and
    your key is gone unless the firmware you flashed carries it (docs/enable-ssh.md).
EOF
  exit 4
fi
echo "   SSH OK (${probe#*$'\n'})"

# ---- diagnostics: from the running host, or from the files on disk if it is
# down; without a usable rcplugd the device packs the raw logs (tar.gz).
if [ $DIAG = 1 ]; then
  stamp="$(date +%Y%m%d-%H%M%S)"
  out="${OUT:-rcplug-diagnostics-$HOST-$stamp.zip}"
  echo "-- collecting diagnostics"
  if ! device diagnostics > "$out" || [ ! -s "$out" ]; then
    rm -f "$out"
    die "could not collect diagnostics from $HOST (is rcplug installed?)" 6
  fi
  if [ "$(head -c 2 "$out")" != "PK" ]; then
    # Raw logs come as tar.gz; do not leave them under a .zip name.
    case "$out" in
      *.zip) mv "$out" "${out%.zip}.tar.gz"; out="${out%.zip}.tar.gz";;
    esac
    echo "   rcplugd could not build the bundle; these are the raw logs (tar.gz)"
  fi
  echo "Saved $out"
  echo "Passwords, keys and the values of settings are masked; review the bundle before sharing it publicly."
  exit 0
fi

# ---- report problems the device has logged since the last visit
check="$(device check 2>/dev/null || true)"
if [ -n "$check" ]; then
  echo "   rcplug on the device: ${check%%$'\n'*}"
  rest="${check#*$'\n'}"
  if [ "$rest" != "$check" ] && [ "$rest" != "logs: no problems found" ]; then
    echo "   The device logged problems:"
    printf '%s\n' "$rest" | head -12 | sed 's/^/     /'
    echo "   Download everything with: $0 --host $HOST --diagnostics"
  fi
fi

# ---- uninstall
if [ $UNINSTALL = 1 ]; then
  if [ $DRYRUN = 1 ]; then echo "[dry-run] would stop rcplug and remove it with all plugin data"; exit 0; fi
  confirm "Remove rcplug and all plugin data from $HOST?"
  device uninstall
  exit 0
fi

# ---- single plugins into an existing installation
if [ ${#PLUGINS[@]} -gt 0 ]; then
  found="$(device find 2>/dev/null)" || die "rcplug is not installed on $HOST; run the wizard without --plugin first" 5
  installed="$(sed -n 's/^dir=//p' <<<"$found")"
  [ -n "$ADDR" ] || ADDR="$(sed -n 's/^addr=//p' <<<"$found")"
  echo "   rcplug found in $installed"
  if [ $DRYRUN = 1 ]; then echo "[dry-run] would install ${PLUGINS[*]} into $installed/plugins/"; exit 0; fi
  confirm "Install ${#PLUGINS[@]} plugin(s) on $HOST?"
  for p in "${PLUGINS[@]}"; do
    name="$(basename "$(cd "$p" && pwd)")"
    echo "-- $name"
    pack "$p" | upload "/tmp/rcplug-plugin-$name.tgz"
    device plugin "$name" "/tmp/rcplug-plugin-$name.tgz"
  done
  echo "Done. Open http://$HOST:${ADDR##*:}/#/plugins"
  exit 0
fi

# ---- full install: a staged payload directory or the release tarball
TGZ=""
if [ -n "$PAYLOAD" ]; then
  [ -e "$PAYLOAD" ] || die "--payload $PAYLOAD does not exist" 5
  [ -f "$PAYLOAD" ] && TGZ="$PAYLOAD"
elif [ -d "$HERE/payload" ]; then
  PAYLOAD="$HERE/payload"
else
  # The newest release tarball next to the wizard (not the examples one).
  TGZ="$(find "$HERE" -maxdepth 1 -name 'rcplug-[0-9]*-aarch64.tar.gz' | sort -V | tail -1)"
  [ -n "$TGZ" ] || die "no payload next to the wizard (run 'make package' or pass --payload)" 5
fi
if [ -n "$TGZ" ]; then
  # List first: "tar | grep -q" fails under pipefail when grep exits early.
  listing="$(tar -tzf "$TGZ" 2>/dev/null)" || die "$TGZ is not a readable tarball" 5
  grep -qx '\(\./\)\{0,1\}rcplugd' <<<"$listing" || die "$TGZ contains no rcplugd" 5
  echo "-- payload: $TGZ"
else
  [ -f "$PAYLOAD/rcplugd" ] || die "no payload at $PAYLOAD (run 'make package' or pass --payload)" 5
  echo "-- payload: $PAYLOAD"
fi

install_args=()
[ -n "$ADDR" ] && install_args+=(--addr "$(sq "$ADDR")")
[ -n "$DIR_OVERRIDE" ] && install_args+=(--dir "$(sq "$DIR_OVERRIDE")")
if [ $DRYRUN = 1 ]; then echo "[dry-run] would push the payload and run install.sh ${install_args[*]+${install_args[*]}}"; exit 0; fi
confirm "Install rcplug on $HOST?"

echo "-- transferring and installing"
if [ -n "$TGZ" ]; then
  upload /tmp/rcplug-payload.tgz < "$TGZ"
else
  pack "$PAYLOAD" | upload /tmp/rcplug-payload.tgz
fi
device install /tmp/rcplug-payload.tgz ${install_args[@]+"${install_args[@]}"}

[ -n "$ADDR" ] || ADDR="$(device find 2>/dev/null | sed -n 's/^addr=//p' || true)"
port="${ADDR##*:}"
port="${port:-8080}"
echo "-- checking the dashboard on http://$HOST:$port/"
ok=0
if command -v curl >/dev/null 2>&1; then
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    # 200, or 401 when a dashboard password is set: either way it answers.
    code="$(curl -sS -o /dev/null -w '%{http_code}' --max-time 3 "http://$HOST:$port/api/system" 2>/dev/null || true)"
    if [ "$code" = 200 ] || [ "$code" = 401 ]; then ok=1; break; fi
    sleep 1
  done
fi
if [ $ok = 1 ]; then
  echo "   dashboard is up"
else
  echo "   (not reachable from here yet; give it a few seconds, then open it in a browser)"
fi
echo ""
echo "Done. Dashboard: http://$HOST:$port/"
echo "The console starts read-only; switch on control in its header if you want it."
