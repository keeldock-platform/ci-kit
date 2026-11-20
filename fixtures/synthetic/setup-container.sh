#!/bin/sh
set -eu
archive=/tmp/ci-home/node.tar.gz
curl -fsSL --proto '=https' --tlsv1.2 https://nodejs.org/dist/v26.8.1/node-v26.8.1-linux-x64.tar.gz -o "$archive"
printf '%s  %s\n' b2b76660fa4ded4e0b2a41ee3c0c651cd52ea8170ead91ebac1e147ac3d55643 "$archive" | sha256sum --check -
mkdir -p /tmp/ci-home/.local
tar -xzf "$archive" --strip-components=1 -C /tmp/ci-home/.local
rm "$archive"
export PATH=/tmp/ci-home/.local/bin:$PATH
node ci/setup.mjs "$1"
