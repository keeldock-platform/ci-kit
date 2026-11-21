#!/bin/sh
set -eu
# Archive mirrors only: live mirrors could silently mix package epochs.
case "$CI_SNAPSHOT" in ????/??/??) ;; *) exit 2 ;; esac
: > /etc/pacman.d/mirrorlist
for mirror in https://umea.archive.pkgbuild.com https://archive.archlinux.org; do
  printf 'Server = %s/repos/%s/$repo/os/$arch\n' "$mirror" "$CI_SNAPSHOT" >> /etc/pacman.d/mirrorlist
done
# Retry downloads, never package transactions or tests. Pacman retains signed
# database/package verification and tries the next dated mirror on failure.
sed -i '/^XferCommand[[:space:]]*=/d' /etc/pacman.conf
sed -i '/^\[options\]/a XferCommand = /usr/bin/curl -q --fail --location --retry 2 --retry-connrefused --retry-delay 5 --retry-max-time 110 --connect-timeout 10 --max-time 90 --speed-time 20 --speed-limit 1024 --output %o %u' /etc/pacman.conf
pacman -Syyuu --noconfirm --needed "$@"
