# Synthetic source fixture

`node fixtures/synthetic/materialize.mjs /absolute/new/directory [scenario]` produces redistributable fake Rust and TypeScript units and two plugin units. Captured pack setup creates report parents after Git checkout. Supported scenarios are success, failure, stdout, stderr, ansi, annotation, crash, timeout and cancel. Every test command first emits the private marker through the capture path. Negative scenarios deliberately cannot pass admission.

Provide `CI_SYNTHETIC_MARKER_FILE` out of band for observer qualification. `CI_SYNTHETIC_SUPPORT_FILE` optionally supplies the reviewed exact installed support contract keyed by platform. All eleven venue slots are declared. Container/mobile execution refuses absent support instead of guessing image packages, SDK versions or device identities. Host success does not qualify browser engines, mobile cases or another OS. Native qualification must exercise the actual venue adapters and cleanup.
