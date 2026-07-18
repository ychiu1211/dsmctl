#!/bin/sh
set -eu

data_dir="${DSMCTL_DATA_DIR:-/opt/dsmctl/data}"
secret_dir="${DSMCTL_SECRET_DIR:-/opt/dsmctl/secrets}"
destination="${1:?usage: backup.sh DESTINATION_TAR_GZ}"

if [ ! -f "$data_dir/gateway.db" ] || [ ! -f "$secret_dir/master.key" ]; then
    echo "gateway.db or master.key is missing" >&2
    exit 1
fi
umask 077
tar -C / -czf "$destination" "${data_dir#/}" "${secret_dir#/}/master.key"
printf 'Encrypted state and its required master key were backed up to %s.\n' "$destination"
