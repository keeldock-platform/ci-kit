# CLI usage

`kd-ci` validates local JSON descriptors, selects checks and observes explicitly registered runs. These prerelease builds report `production_qualified:false`. Native qualification and hosted integration evidence are required before a release can qualify.

Use `kd-ci --help` and `kd-ci COMMAND --help` for the current command and flag reference. The binary provides `version`, `doctor`, `list`, `explain`, `run`, `dispatch`, `status`, `wait`, `failures`, `cancel`, `results purge` and shell `completion` commands. Help and completions are text; other commands support `--json`. JSON emits one closed result followed by a newline. Bundled schemas define the result envelope and command-specific local data.

Local configuration uses strict UTF-8 JSON. Unknown versions, duplicate or unknown keys and unsafe paths are rejected. Existing operator policy supplies permitted tooling and destinations. Configuration cannot create authority. `doctor` checks prerequisites and never installs or changes settings.

A local run is feedback. Specify an alias or an exact check/platform identity; `--check`, `--platform`, `--variant AXIS=VALUE` and `--shard INDEX/COUNT` narrow feedback. Quick feedback requires exact available base/head objects and a clean matching checkout. Related-test uncertainty runs the whole selected unit. Complete admission uses every required variant and shard. All local results are non-admitting.

`--offline` never authorises a download or installation. Local execution requires existing tools and dependencies; remote commands refuse before resolving authentication. Offline operation cannot refresh revocation policy.

Remote access uses existing GitHub CLI authentication and explicit reviewed routing. Dispatch preserves its UUID and never retries a POST automatically. A lost response requires reconciliation; it does not establish that a run started. Status binds the full saved request and a specific attempt. Missing local expectations cannot establish admission.

Wait interruption detaches without cancelling. Use `cancel` explicitly to request cancellation; a request acknowledgement is distinct from an observed cancelled conclusion. Cancellation is run-scoped at the provider, so an attempt change is reported as unknown.

Captured local output remains in the configured ignored private result directory. JSON contains a local log directory and child exit/signal only for local work. Hosted output contains fixed public slots and platforms. `results purge --older-than 168h` deletes only recognised inactive captures; unknown contents remain.

Exit categories are: 0 success/accepted request, 1 executed check/cleanup failure, 2 invalid usage/configuration or missing prerequisite, 3 authority/policy refusal, 4 deadline, 5 confirmed runner capacity, 6 unknown/incomplete evidence, 7 interrupted/cancelled, 8 queued/running status. Dispatch acceptance is not a test verdict.

Updates use separately verified version directories and an explicit path switch. The CLI has no updater, login, setup, release, promotion or arbitrary execution command.
