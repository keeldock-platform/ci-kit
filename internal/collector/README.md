# Exact result reconciliation

`Evaluate` compares one immutable run/attempt with its expected fixed platform
slots and finalizer. It requires terminal successful native dependencies and the
same complete provider job set. Missing, duplicate, unexpected, skipped,
cancelled, blocked, neutral, zero or unknown evidence fails. The first actual
failure remains available even when fail-fast later cancels other jobs. The
caller binds source/base/control/request identity before this pure comparison.
REST consumers may project native dependency results only when the exact trusted
finalizer is present and its reviewed implementation checks actual `needs`.

`ValidateReports` reconciles private unit/check/platform/variant/shard tuples,
report paths, test IDs and assertion IDs. JUnit semantics come from the maintained
`github.com/joshdk/go-junit` dependency, with a bounded standard XML safety scan;
no external entities/directives are accepted. Every report must be nonempty,
well formed, bounded, all-passed and exactly match the expected census. Assertions
must name actual parsed passed tests. Shards must form a complete disjoint union;
only explicit feedback mode permits a selected shard subset. Quick feedback must
supply its exact selected test census and remains non-admitting.

Errors contain fixed classes only. Test names, report contents and private tuples
are never suitable hosted output. `PublicLine` emits only an allowed fixed slot
and PASS/FAIL. These local APIs do not certify any unexecuted native or hosted
qualification gate.
