#!/bin/sh
set -eu

data_dir="${DSMCTL_DATA_DIR:-/opt/dsmctl/data}"
secret_dir="${DSMCTL_SECRET_DIR:-/opt/dsmctl/secrets}"
uid="${DSMCTL_UID:-10001}"
gid="${DSMCTL_GID:-10001}"

umask 077
install -d -m 0700 -o "$uid" -g "$gid" "$data_dir" "$secret_dir"
if [ ! -f "$secret_dir/master.key" ]; then
    dd if=/dev/urandom of="$secret_dir/master.key" bs=32 count=1 status=none
fi
if [ ! -f "$secret_dir/bootstrap" ]; then
    bootstrap="$(od -An -N32 -tx1 /dev/urandom | tr -d ' \n')"
    printf '%s\n' "$bootstrap" > "$secret_dir/bootstrap"
    printf 'One-time bootstrap token (store it now): %s\n' "$bootstrap"
fi
chown "$uid:$gid" "$secret_dir/master.key" "$secret_dir/bootstrap"
chmod 0600 "$secret_dir/master.key" "$secret_dir/bootstrap"
printf 'Prepared %s and %s for UID:GID %s:%s.\n' "$data_dir" "$secret_dir" "$uid" "$gid"
