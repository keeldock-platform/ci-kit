# Captured processes

`Run` executes absolute argv directly. The trusted caller resolves executables to
reviewed tools or committed scripts and supplies a canonical checkout root, a
relative working directory, a new capture directory and a bounded timeout.
Capture files are opened before launch and each stream stops at 64 MiB; output is
never echoed or tailed. The environment admits ordinary build values and fixed
offline settings, and excludes inherited credentials, agent sockets, workflow
command files, preload hooks and debug channels. `RunAcquisition` is solely for
the trusted Git adapter's fixed isolation values and selected inherited token.

Unix children have an owned process group. Exit observation retains the leader
with `waitid(WNOWAIT)` until group signals finish, preventing reuse of the group
leader ID while signals are sent. Windows children start suspended, join a
kill-on-close Job Object, then resume their primary thread. Cleanup sends a
graceful stop, force terminates, reaps the direct child, drains capture and checks
the group/job is empty within a 30-second cleanup bound. Failure preserves the
original direct child exit code/signal and records incomplete cleanup separately.
Escaped processes are outside this ownership contract; this is not a sandbox.

Windows directory ACLs are protected and grant only the current user, with object
and container inheritance so ordinary tool-created descendants stay user-only.
Capture files receive explicit protected user-only ACLs at creation. Unix uses
0700 directories and 0600 files. Trees must be exclusively owned against concurrent
mutation while paths are checked and processes execute.

`RunHelper` is the narrow trusted authentication exception: its caller supplies a
bounded private memory writer, and it uses the same lifecycle with closed stdin
and discarded stderr. Project commands always use `Run`.

Local source tests cover capture bytes, exit/crash, timeout, output exhaustion,
authority stripping, descendants, cancellation, paths, purge and native ACLs.
Native execution on each supported OS/architecture is required qualification;
cross-compilation or the presence of a test is not runtime proof.

`RunTool` is a separate bridge only for the reviewed maintained Android emulator
action's pinned lifecycle functions through the exact trusted kit
`packs/android-emulator/lifecycle.cjs`, executed with approved Node outside
the private source. Its caller verifies the bundle's exact immutable revision.
Every permitted action input is explicit and strictly bounded; the script is
exactly `"$CI_KIT_HELPER" native-hook`, with no source arguments or pre-launch
script. The helper is the current executable, and its state file is user-only.
This bridge cannot pass source credentials or runner command-file channels and
does not expand the ordinary project environment. Explicit simulator UUIDs and
canonical emulator serials are typed; ambient selectors are stripped.

The raw upstream `lib/main.js` entrypoint is refused because it can continue to
emulator launch after a failed pre-launch hook. The small trusted lifecycle
adapter must verify preparation before launch and invoke maintained SDK/AVD/kill
functions, with cleanup in finally. Its caller binds the exact kit revision and
pinned upstream bundle before RunTool; this API checks fixed paths and inputs,
not remote provenance or platform readiness.

Result.Interrupted records cancellation only when the first observed completion
cause is the active command context's cancellation. Result.ExitObserved records
an observed child exit before stop/cleanup. Later cancellation during cleanup does
not overwrite that first cause or the preserved raw exit/signal. Callers classify
cleanup failure separately. RUSTUP_AUTO_INSTALL=0 is admitted only as an explicit
fixed tool-control value; inherited or enabling values are refused.

Windows additionally retains exactly INCLUDE, LIB, LIBPATH and the x64/ARM64
MSVC target linker variables set by trusted tool preparation. No arbitrary Cargo
prefix, Rust flags, wrapper or workflow-channel inheritance is admitted.
