# Exact result reconciliation

`Evaluate` compares one immutable run/attempt with its expected fixed platform
slots and finalizer. It requires terminal successful native dependencies and the
same complete provider job set. Missing, duplicate, unexpected, skipped,
cancelled, blocked, neutral, zero or unknown evidence fails. The first actual
failure remains available even when fail-fast later cancels other jobs. The
caller binds source/base/control/request identity before this pure comparison.
REST consumers may project native dependency results only when the exact trusted
finalizer is present and its reviewed implementation checks actual `needs`.

`ValidateReports` reconciles required private unit/check/platform/variant/shard
identities and report paths. Maintained runners own bulk test discovery and
pass/fail. Every supported JUnit report must be fresh, bounded and nonempty,
with actual passed cases and no failure/error. An all-skipped suite refuses;
ordinary product skips/retries and renamed tests no longer require exact census
equality. Unsupported reports refuse. Required shards remain complete; explicit
feedback can select one shard. Named selection maps remain for quick tests and
measured shards, independently of admission completeness.

`assertions` declares only mandatory security/privacy invariants, including
positive private canaries. Each must pass exactly once, never skip. Unit-level
invariant ownership remains checked. These lists are optional for ordinary product
suites. Runner exit failures take precedence over report validation. The XML
resource/entity/encoding guards remain, and private reports never become public
results. `Failures` extracts names and errors only for private-file diagnostics.

Errors contain fixed classes only. Test names, report contents and private tuples
are never suitable hosted output. `PublicLine` emits only an allowed fixed slot
and PASS/FAIL. These local APIs do not certify any unexecuted native or hosted
qualification gate.
