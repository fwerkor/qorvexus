#!/bin/sh
set -eu

CONFIG_PATH="/data/qorvexus.yaml"

if [ ! -f "$CONFIG_PATH" ]; then
  /usr/local/bin/qorvexus init --path "$CONFIG_PATH"
fi

if grep -q 'address: 127.0.0.1:7788' "$CONFIG_PATH"; then
  sed -i 's/address: 127.0.0.1:7788/address: 0.0.0.0:7788/' "$CONFIG_PATH"
fi

exec /usr/local/bin/qorvexus start --config "$CONFIG_PATH"
