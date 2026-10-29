#!/usr/bin/env bash
# Fetch BertoldVdb/WACResearch, apply our patch, build bin/airplay-wifi-setup.
set -euo pipefail
cd "$(dirname "$0")"
UPSTREAM=https://github.com/BertoldVdb/WACResearch.git
PIN=231b78aab56fe796b834af2151d87feb2877b4af   # commit the patch was written against
if [ ! -d build/.git ]; then
  git clone -q "$UPSTREAM" build
  git -C build checkout -q "$PIN" || echo "warning: pinned commit not found, using default branch"
  git -C build apply ../patches/0001-persistent-tcp-connection.patch
fi
mkdir -p bin
go build -o bin/airplay-wifi-setup ./cmd/airplay-wifi-setup
echo "built bin/airplay-wifi-setup"
