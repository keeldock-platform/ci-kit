# Registered source acquisition

`ParseMap` validates a strict, case-sensitive JSON configuration of at most 16 KiB
and 32 source aliases. Root slot is `SOURCE_READ_01`; all source identities,
repository IDs and secret slots are unique. The map contains no token values.

`Acquire` requires trusted repository-identity and current admitted-base signature
verifiers. It acquires the exact root before reading its immutable composition,
then admits only registered locked dependencies sorted by alias. The requested
base must equal the current registered target branch; exact HEAD, ancestry and
canonical origin are checked. Fetch depth is bounded to 1024 commits; missing
ancestry fails closed and shallow history is never selective-admission proof.

Git runs under capture with system/global configuration, helpers, headers,
rewrites, hooks, recursive submodules, LFS and progress disabled. The selected
single-source credential reaches only the fixed credential helper through
inherited environment and Git's credential protocol. It never enters argv, URLs,
persistent configuration or returned values. The helper checks HTTPS origin and
repository path before answering. No project command receives this environment.
Debug acquisition is refused. Other inherited slots are absent from Git's env.

All failure paths remove owned trees and retain a cleanup failure. Successful
callers must `Close` the checkout after pack cleanup. VM disposal remains the
hosted persistence boundary. Trusted source and dependencies can still read
plaintext or use network/runner channels; capture does not establish confinement.
Actual private acquisition and native credential/ACL behavior require separate
synthetic hosted qualification before enablement.

Git interprets credential.helper as its own command protocol even when `-c` is a
safe argv element. The adapter uses the fixed `!"absolute helper path"` form so a
quoted path is not mistaken for a named git-credential helper. Only a canonical
trusted absolute executable path is accepted; control and shell-expansion
characters are refused, and Git supplies the fixed get/store/erase operation.
A native synthetic credential-fill regression covers spaces and Unicode without
network, persistent credentials or a second helper build.
