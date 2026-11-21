# web-linux

The platform adapter validates the native venue and runs the selected private command vectors through the captured pack engine. Platform maintainers own this directory. Product manifests and captured output do not belong here.

The adapter keeps the pinned Playwright 1.65.1 image and driver, with an official
Firefox 1575 overlay for the upstream ARIA reflection crash (Playwright #43967,
#44135 and roll #43982). `packs/firefox.go` pins its HTTPS archive and SHA-256,
bounds transfer/extraction and refuses symlinks, traversal or fallback. The
expected 1572 lookup aliases the verified 1575 tree; readiness verifies the real
executable path. Chromium/WebKit stay image-provided. Compatibility is covered
by the opt-in public container regression; launch alone is not proof. The
overlay is ephemeral and never saved to cache or artifact.

Container cleanup owns the Docker instance. The outer private-work lifecycle owns
the overlay directory and removes it after the container closes; failed overlay
preparation removes its own newly created directory. A preexisting directory
refuses without changing it.
