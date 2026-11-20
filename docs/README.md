# Tooling usage

Run `kd-ci --help` for the command reference. `kd-ci version --json` reports schema and kit compatibility and qualification status. Each consuming checkout supplies its own configuration and descriptors. Run `kd-ci list` to discover those descriptors; run an alias or canonical check on its supported local platform.

Use the versioned schemas shipped with the matching tool version. Updates are explicit reviewed version changes. Runtime workflow references use complete commit SHAs. The command does not install prerequisites or update itself.

Publication requires the layout and external-policy checks in `tests/policy`. A missing policy or any content match blocks publication.

`revoked-signer-fingerprints` is the canonical current-kit revocation ledger. An empty committed file records no revoked fingerprints. Signature verification requires the exact pinned blob and matching live bytes; absent, changed or malformed ledgers fail closed.
