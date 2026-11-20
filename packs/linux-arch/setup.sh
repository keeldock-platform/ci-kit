#!/bin/sh
set -eu
# One official dated archive and one complete upgrade avoid partial upgrades.
case "$CI_SNAPSHOT" in ????/??/??) ;; *) exit 2 ;; esac
printf 'Server = https://archive.archlinux.org/repos/%s/$repo/os/$arch\n' "$CI_SNAPSHOT" > /etc/pacman.d/mirrorlist
pacman -Syyuu --noconfirm --needed "$@"
