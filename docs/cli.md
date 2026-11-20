# CLI usage

`kd-ci` validates local JSON descriptors, selects checks and observes explicitly registered runs. These prerelease builds report `production_qualified:false`. Native qualification and hosted integration evidence are required before a release can qualify.

Use `kd-ci --help` and `kd-ci COMMAND --help` for the current command and flag reference. The binary provides `version`, `doctor`, `list`, `explain`, `run`, `dispatch`, `status`, `wait`, `failures`, `cancel`, `results purge` and shell `completion` commands. Help and completions are text; other commands support `--json`. JSON emits one closed result followed by a newline. Bundled schemas define the result envelope and command-specific local data.

Local configuration uses strict UTF-8 JSON. Unknown versions, duplicate or unknown keys and unsafe paths are rejected. Existing operator policy supplies permitted tooling and destinations. Configuration cannot create authority. `doctor` checks prerequisites and never installs or changes settings.

A local run is feedback. Specify an alias or an exact check/platform identity; `--check`, `--platform`, `--variant AXIS=VALUE` and `--shard INDEX/COUNT` narrow feedback. Quick feedback requires exact available base/head objects and a clean matching checkout. Related-test uncertainty runs the whole selected unit. Complete admission uses every required variant and shard. All local results are non-admitting.

`--offline` never authorises a download or installation. Local execution requires existing tools and dependencies; remote commands refuse before resolving authentication. Offline operation cannot refresh revocation policy.

Remote access uses existing GitHub CLI authentication and explicit reviewed routing. Dispatch preserves its UUID and never retries a POST automatically. A lost response is non-admitting unknown. The client neither searches for a recent run nor retries the POST; manual operator reconciliation is required before any fresh request. Status binds the full saved request and a specific attempt. Missing local expectations cannot establish admission.

`status` and the retained `wait` command each make one bounded observation; no watcher file or polling loop remains. Use `cancel` explicitly to request cancellation; a request acknowledgement is distinct from an observed cancelled conclusion. Cancellation sends one POST between bound before/after reads and stores one compact user-only outcome. A still-running result or lost acknowledgement remains unknown; the command never polls for confirmation. Cancellation is run-scoped at the provider, so an attempt change is reported as unknown.

Captured local output remains in the configured ignored private result directory. JSON contains a local log directory and child exit/signal only for local work. Hosted output contains fixed public slots and platforms. `results purge --older-than 168h` deletes only recognised inactive captures; unknown contents remain.

Exit categories are: 0 success/accepted request, 1 executed check/cleanup failure, 2 invalid usage/configuration or missing prerequisite, 3 authority/policy refusal, 4 deadline, 5 confirmed runner capacity, 6 unknown/incomplete evidence, 7 interrupted/cancelled, 8 queued/running status. Dispatch acceptance is not a test verdict.

Updates use separately verified version directories and an explicit path switch. The CLI has no updater, login, setup, release, promotion or arbitrary execution command.

`kd-ci failures --private-report REPORT.xml --private-output FAILURES.json`
reads an existing local user-only JUnit report and creates a new user-only JSON
file listing each failed test by name and error. Its parent directory must be
user-only; links and overwrites refuse. Names/errors never enter CLI stdout,
stderr or the public result. The command refuses CI/workflow environments. The
normal `failures RUN_ID --concern ALIAS` command still returns only safe hosted
status; hosted raw reports remain ephemeral and are never uploaded or fetched
through a public artifact/log path.

Completeness follows the 2026-11-20 owner decision: trust maintained runners plus
fresh bounded nonzero results and every required suite/platform/variant/shard.
Bulk exact test-name/assertion equality is retired. Optional `census` metadata
continues to drive related-test and measured-shard selection. `assertions` is the
explicit mandatory privacy/security invariant list; those tests must pass once.
All selectors, Nx planning, local execution and CLI commands remain available.
