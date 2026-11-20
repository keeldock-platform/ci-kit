# Unsigned CLI candidate checks

Owner: CI maintainers. Used only by the manual source-free candidate workflow. Confirms the native host, inspects the executable format and Go build settings, runs the actual executable's version/help/offline-refusal checks, then copies exactly those checked bytes with a checksum, the reviewed third-party legal notices and a bounded public manifest.

The workflow uses the reviewed GoReleaser OSS snapshot builder. Uploads contain only unsigned public tooling, reviewed legal notices, their checksum and the producing control SHA/run/attempt metadata. Runtime qualification remains false. This helper must not acquire private source, read product configuration, publish releases, install tooling for users or access signing credentials. Native candidate execution is a future hosted check, separate from private archive packaging.

The control checkout must contain the reviewed `THIRD_PARTY_NOTICES` file. The helper copies only that fixed legal file, bounds its size, refuses links, and binds its digest in both the manifest and SHA256SUMS. Missing notices fail the candidate step.
