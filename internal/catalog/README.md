# Private execution catalogue

Owner: CI maintainers. Validates feature-local JSON and expands each check, platform, declared variant and measured whole-test shard into a deterministic execution entry. Commands remain argument vectors. Descriptors and their expanded contents belong in private source or local results. This package must not contain product names, source repository mappings, credentials or publication destinations.

Configured discovery respects the declared feature directories and shared-file path for both working trees and immutable objects. Private aggregate aliases live in that shared file and use maintained DAG validation before deterministic expansion; they never become a second feature inventory.

Composition assembly uses each source's own configuration, combines descriptors before cross-source validation, and keeps declared IDs unchanged. The immutable API reads exact existing Git objects without fetching or guessing local sibling paths. Local and hosted callers use that same assembly before computing opaque selector indices.
