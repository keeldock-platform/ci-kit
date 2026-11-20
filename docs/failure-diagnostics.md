# Failure diagnostics

Source-free bootstrap actions print ordinary tool output, failure exit status,
OS/architecture and version facts. The public signature smoke clones only the
public kit and prints both captured streams, argv/results and Git trace2 on
failure. It is enabled before the source-secret step on the synthetic route.
`tests/integration/test_diagnostics_policy.py` checks the public set and rejects
source-reading additions. Source acquisition and every source-aware process stay
captured, including SDK/device commands that run after checkout.

Before acquisition the trusted helper reads `.github/diagnostics.json` from the
pinned public control checkout. It preserves exact argv/cwd/exit/timeout/cleanup,
both captured streams and fresh JUnit, including failed runner output. Android's
second helper uses the same private sink. No environment dump or source files are
collected. Capture copies have a 128 MiB aggregate budget; truncation/read failure
is explicit in encrypted metadata. Failed report parsing preserves log tails.

On failure, age v1.3.2 (module plus checksum pinned in go.mod/go.sum) encrypts the
bounded JSON envelope to the configured native recipient. Only after successful
atomic sealing does the trusted Execute/Plan step set `diagnostics=ready`. The
one-day uploader accepts one exact `.age` path and requires that signal. Missing,
preexisting, invalid or incomplete ciphertext publication refuses; plaintext is
never an upload fallback. No artifact is made on success. Public logs retain
fixed outcomes for all source-bearing work.

The coordinator's dedicated identity is `~/.config/keeldock-ci/diag-age.key`,
user-only, never in a repository or runner. Back it up only through the owner's
normal private key custody. Rotation generates a new identity, changes the public
recipient in a reviewed signed kit and retains old identities locally until the
one-day evidence expires. Losing the key loses the diagnostic evidence.

`kd-ci failures RUN --concern ALIAS` verifies its existing immutable receipt and
provider job/attempt, downloads only exact failed private-slot artifacts, follows
the signed storage URL without forwarding credentials and decrypts locally.
Missing, expired, duplicate, wrong-run/attempt/source/kit, oversized, corrupt or
wrong-key evidence refuses. Failed source-free steps have no private artifact;
the ordinary public job log carries their errors. Detailed evidence is written
to new user-only `failures-RUN-ATTEMPT-PLATFORM.json` files in the private result directory.
Use `--private-output PATH` to select another user-only filename prefix. Artifacts are processed one platform at a time; a large valid platform never consumes another platform's download budget. Stdout/stderr
and JSON status contain safe metadata only. Inspect the local report for failing
tests, errors, command outcomes and 16 KiB stream tails; the encrypted bundle holds
full captured streams within the stated budget.

A recorded fixture is inspected with `kd-ci failures --private-bundle FILE.age
--identity ~/.config/keeldock-ci/diag-age.key --private-output REPORT.json`.
Local decryption refuses CI and hosted-output environments. Neither transport nor
decryption can admit a candidate or execute the stored command.

| Threat | Control and limit |
| --- | --- |
| Spoofed/changed run | Existing receipt, provider source/control tuple, attempt-specific slot and decrypted envelope checks; no admission from diagnostics |
| Ciphertext tampering | Maintained age authentication, read to EOF before rendering, wrong-key/tamper/truncation negatives |
| Plaintext or key disclosure | Captured-only private children, pre-source recipient binding, affirmative successful seal output, exact ciphertext-only upload, canary positive/negative tests |
| Malformed/oversized evidence | Bounded file handles, aggregate budget, closed envelope/command/path schema, no archive extraction or command execution |
| Availability | One-day evidence expiry and bounded requests; missing diagnostics stay explicit failure |

The existing ordinary CI residual remains: native workloads run as the runner
user and can maliciously mutate control binaries, files or network channels.
Encryption and output capture prevent accidental publication; they are not a
same-user sandbox or authenticated evidence of honest source code. The public
recipient lets anyone construct ciphertext, so provider/producer binding and
non-admitting display remain essential. No claim of hostile-code confidentiality
or origin authentication follows from age alone.

Synthetic coordinator readback uses `kd-ci failures RUN --experiment-receipt
RECEIPT.json [--attempt N] [--identity PATH] [--private-output PATH]`. The receipt
is a user-only JSON object `{branch, expectation}`; expectation is the existing
versioned provider receipt shape, bound to the exact canary dispatch inputs,
caller/control/kit/source SHAs, repository/workflow IDs, run and attempt. The
reader permits only the fixed synthetic caller repository, actor, workflow,
experiment branch syntax and exactly the four diagnostic platforms. It verifies
the immutable caller workflow's actual request/kit reference and all five
provider jobs. This is read-only: production main-only validation and dispatch
remain unchanged. A copied ciphertext without that receipt proves local
crypto/parsing only, never a hosted source/control tuple.
