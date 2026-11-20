# Selection acceptance

Owner: CI maintainers. Package tests in `internal/catalog` and `internal/planner` verify strict descriptors, exact check/platform isolation, declared variants, whole-test shards, conservative selection and maintained graph integration. Run with `go test ./internal/catalog ./internal/planner`. Nx tests require the reviewed `planner` dependencies pre-provisioned with `npm ci --ignore-scripts` and skip explicitly when absent. Skips never qualify affected admission.
