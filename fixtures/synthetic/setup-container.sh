#!/bin/sh
set -eu
archive=/tmp/ci-home/node.tar.xz
curl -fsSL --proto '=https' --tlsv1.2 https://nodejs.org/dist/v26.8.1/node-v26.8.1-linux-x64.tar.xz -o "$archive"
printf '%s  %s\n' 3e301118d7df53d563b7e96c1617545f26e2f76f9724be668d6cab65c15dda5d "$archive" | sha256sum --check -
mkdir -p /tmp/ci-home/.local
tar -xJf "$archive" --strip-components=1 -C /tmp/ci-home/.local
rm "$archive"
export PATH=/tmp/ci-home/.local/bin:$PATH
node ci/setup.mjs "$1"
