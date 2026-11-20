#!/bin/sh
set -eu
# The committed private support lock supplies a dated, signed official archive
# and exact package versions. This script only runs inside the owned container.
case "$CI_SNAPSHOT" in ????????T??????Z) ;; *) exit 2 ;; esac
rm -f /etc/apt/sources.list /etc/apt/sources.list.d/debian.sources
printf 'Types: deb\nURIs: http://snapshot.debian.org/archive/debian/%s/\nSuites: trixie\nComponents: main\nSigned-By: /usr/share/keyrings/debian-archive-keyring.gpg\nCheck-Valid-Until: no\n' "$CI_SNAPSHOT" > /etc/apt/sources.list.d/ci.sources
apt-get update
DEBIAN_FRONTEND=noninteractive apt-get install --yes --no-install-recommends "$@"
