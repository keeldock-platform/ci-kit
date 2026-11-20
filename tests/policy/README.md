# Publication policy

`public_content.py` scans tracked paths, file contents, index blobs and every outgoing commit using an externally supplied JSON policy. The policy has `schema_version: 1` and nonempty `terms` and `markers` arrays; keep it outside the public checkout. Missing policy, unsupported content or any match fails. Output contains a fixed result; an optional private report stays outside the checkout.

Run the scan before publishing. `pre-push` is the candidate-branch publishing hook; it never dispatches workflows. Synthetic tests use invented terms only.

`workflow_lint.py` passes published YAML through actionlint with a lint-only compatibility projection for the pinned validator: root `cache-mode: none` becomes a blank line, and native `$/` composite references use the same local action metadata. Runtime YAML is unchanged; other values and diagnostics remain errors. This handles documented provider syntax newer than actionlint 1.7.12. Native action locks independently verify action identity.

The current-kit workflow list comes from the layout role map. Source-free kit policy lints these entries and composites; unchanged legacy workflow findings are reported separately during migration. The publication content check still scans the complete tree and outgoing history without a baseline exemption. The complete clean kit uses literal full-SHA actions inside same-revision composites and verifies their native locks. This supplies immutable execution when native enforcement is unavailable; deployment still requires independent review of the account controls. External reusable workflows use full-SHA refs. Native locks bind reviewed full commit SHAs and numeric identities; external reusable workflows use full-SHA refs.
