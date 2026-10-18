#!/bin/sh
# rcplug-remote.sh: the device side of every rcplug setup tool (the wizard,
# rcplug-setup and rcplug-setup.cmd). It is sent over SSH and run with
#
#   ssh root@<device> sh -s -- <command> [arguments] < rcplug-remote.sh
#
# so nothing has to be on the device beforehand. POSIX sh (bash as sh on the Duo).
#
# Commands:
#   device                  print hostname= and firmware= of the device itself,
#                           read where the stock image keeps them; works
#                           without rcplug installed
#   find                    print dir=, addr=, version= of the installation
#   check                   whether the host answers, and problems in its logs
#   install <tgz> [args]    unpack a payload tarball that was copied to the
#                           device and run its install.sh with args; the
#                           tarball is removed afterwards
#   plugin <name> <tgz>     install or update one plugin from a tarball of its
#                           directory (data is kept), rescan and restart it
#   diagnostics             write the diagnostics zip to stdout; if rcplugd
#                           cannot build it, a tar.gz of the raw logs instead
#   uninstall               stop rcplug and remove it with all plugin data
#   addkey <public key>     add an SSH public key to the user's authorized_keys
#                           unless it is there already
#
# Exit status: 0 ok, 3 rcplug is not installed, other values on errors.
set -u

INITDIR="${RCPLUG_INITDIR:-/etc/init.d}"

fail() { echo "rcplug: $*" >&2; exit 1; }

# locate sets dir and addr of the installation (dir is empty if none).
locate() {
  dir=""; addr=""
  if [ -f "$INITDIR/rcplug" ]; then
    dir="$(sed -n 's/^RCPLUG_DIR="\(.*\)"$/\1/p' "$INITDIR/rcplug")"
    addr="$(sed -n 's/^RCPLUG_ADDR="\(.*\)"$/\1/p' "$INITDIR/rcplug")"
  fi
  # Without the init script, look in the default directory (only on a real
  # device, not in a test sandbox).
  if { [ -z "$dir" ] || [ ! -d "$dir" ]; } && [ -z "${RCPLUG_INITDIR:-}" ]; then
    dir=""
    if [ -x /usr/local/rcplug/rcplugd ]; then dir=/usr/local/rcplug; fi
  fi
  addr="${addr:-:8080}"
}

need_install() {
  locate
  [ -n "$dir" ] || { echo "rcplug is not installed" >&2; exit 3; }
}

cmd="${1:-}"
[ $# -gt 0 ] && shift

case "$cmd" in
  device)
    sys="${RCPLUG_SYSROOT:-}"
    echo "hostname=$(cat "$sys/etc/hostname" 2>/dev/null)"
    echo "firmware=$(cat "$sys/usr/bin/rcp-os.version" 2>/dev/null)"
    ;;

  find)
    need_install
    echo "dir=$dir"
    echo "addr=$addr"
    echo "version=$(cat "$dir/VERSION" 2>/dev/null || echo unknown)"
    ;;

  check)
    need_install
    if [ -x "$dir/rcplugd" ]; then
      "$dir/rcplugd" --check --root "$dir" --addr "$addr"
      exit $?
    fi
    echo "host: missing ($dir/rcplugd)"
    exit 2
    ;;

  install)
    tgz="${1:-}"
    if [ -z "$tgz" ] || [ ! -f "$tgz" ]; then fail "install needs the payload tarball on the device"; fi
    shift
    t="/tmp/rcplug-inst.$$"
    rm -rf "$t"; mkdir -p "$t" || fail "cannot create $t"
    if ! tar -xzf "$tgz" -C "$t"; then rm -rf "$t" "$tgz"; fail "the payload tarball is damaged"; fi
    rm -f "$tgz"
    [ -f "$t/install.sh" ] || { rm -rf "$t"; fail "the tarball contains no install.sh"; }
    sh "$t/install.sh" "$@"
    rc=$?
    rm -rf "$t"
    exit $rc
    ;;

  plugin)
    name="${1:-}"; tgz="${2:-}"
    case "$name" in ''|[!a-z0-9]*|*[!a-z0-9_-]*) fail "invalid plugin name '$name'";; esac
    if [ -z "$tgz" ] || [ ! -f "$tgz" ]; then fail "plugin needs the tarball on the device"; fi
    need_install
    target="$dir/plugins/$name"
    stage="$dir/.staging-$name"
    rm -rf "$stage"; mkdir -p "$stage" || fail "cannot create $stage"
    if ! tar -xzf "$tgz" -C "$stage"; then rm -rf "$stage" "$tgz"; fail "the plugin tarball is damaged"; fi
    rm -f "$tgz"
    [ -f "$stage/plugin.json" ] || { rm -rf "$stage"; fail "the tarball has no plugin.json"; }
    # The code is replaced; the plugin's data lives in data/<name> and stays.
    rm -rf "$target"
    mv "$stage" "$target" || fail "installing $name failed"
    chmod -R a+rX "$target" 2>/dev/null
    if [ -f "$target/$name" ]; then chmod 0755 "$target/$name"; fi
    find "$target" -type f -name '*.sh' -exec chmod 0755 {} + 2>/dev/null
    "$INITDIR/rcplug" reload >/dev/null 2>&1 || echo "rcplug: rescan failed; press Rescan on the dashboard" >&2
    sleep 1
    # A plugin that was running keeps its old process until restarted. 404
    # means it is new or disabled: nothing to restart.
    out="$("$dir/rcplugd" --call "POST /api/plugins/$name/restart" --addr "$addr" 2>&1)"
    case "$out" in
      *'"ok"'*|*'404'*) ;;
      *) echo "rcplug: restarting the host to load $name ($out)" >&2; "$INITDIR/rcplug" restart >/dev/null 2>&1 ;;
    esac
    echo "installed $name in $target"
    ;;

  diagnostics)
    need_install
    t="/tmp/rcplug-diag.$$"
    # Build into a file first, so a failure cannot leave half a zip on stdout.
    if [ -x "$dir/rcplugd" ] && "$dir/rcplugd" --diagnostics "$t.zip" --root "$dir" --addr "$addr" 2>/dev/null && [ -s "$t.zip" ]; then
      cat "$t.zip"; rc=$?; rm -f "$t.zip"; exit $rc
    fi
    rm -f "$t.zip"
    mkdir -p "$t"
    cp -r "$dir/logs" "$t/" 2>/dev/null
    cp "$dir/state.json" "$dir/VERSION" "$t/" 2>/dev/null
    dmesg > "$t/dmesg.txt" 2>&1
    ps > "$t/ps.txt" 2>&1
    cat /proc/version > "$t/version.txt" 2>&1
    df > "$t/df.txt" 2>&1
    tar -czf - -C "$t" .
    rc=$?
    rm -rf "$t"
    exit $rc
    ;;

  stop|start)
    # The emergency brake without a shell: stop rcplug and all plugins (until
    # it is started again or the device restarts), or start it again.
    need_install
    [ -x "$INITDIR/rcplug" ] || fail "rcplug has no init script here"
    "$INITDIR/rcplug" "$cmd" || exit $?
    if [ "$cmd" = stop ]; then
      echo "rcplug stopped (until it is started again or the device restarts)"
    else
      echo "rcplug started"
    fi
    ;;

  uninstall)
    locate
    if [ -x "$INITDIR/rcplug" ]; then "$INITDIR/rcplug" stop >/dev/null 2>&1 || true; fi
    if [ -z "${RCPLUG_INITDIR:-}" ] && command -v update-rc.d >/dev/null 2>&1; then
      update-rc.d -f rcplug remove >/dev/null 2>&1 || true
    fi
    rm -f "$INITDIR/rcplug" /var/log/rcplug.log /var/log/rcplug.log.1
    remove() { case "$1" in */rcplug) if [ -d "$1" ]; then rm -rf "$1" && echo "removed $1"; fi;; esac; }
    [ -n "$dir" ] && remove "$dir"
    if [ -z "${RCPLUG_INITDIR:-}" ]; then
      remove /usr/local/rcplug
    fi
    echo "rcplug uninstalled"
    ;;

  addkey)
    # The key arrives as words (ssh joins the arguments with spaces).
    k="$*"
    case "$k" in
      ssh-*" "*|ecdsa-*" "*|sk-*" "*) ;;
      *) fail "that is not an SSH public key" ;;
    esac
    blob="$(printf '%s\n' "$k" | cut -d' ' -f2)"
    umask 077
    mkdir -p "$HOME/.ssh" || fail "cannot create $HOME/.ssh"
    if [ -f "$HOME/.ssh/authorized_keys" ] && grep -qF -- "$blob" "$HOME/.ssh/authorized_keys"; then
      echo "the key is already on the device"
    else
      printf '%s\n' "$k" >> "$HOME/.ssh/authorized_keys" || fail "cannot write $HOME/.ssh/authorized_keys"
      echo "key added"
    fi
    ;;

  *)
    echo "usage: sh -s -- {device|find|check|install <tgz> [args]|plugin <name> <tgz>|diagnostics|uninstall|addkey <key>}" >&2
    exit 2
    ;;
esac
