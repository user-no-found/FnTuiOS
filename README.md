# FnTuiOS

FnTuiOS packages a Chinese TUIOS interface for the fnOS desktop. The desktop
entry opens an iframe under `/app/tuios/`; the authenticated NAS user owns the
terminal process, groups, home, configuration and sessions. Browser traffic
uses the NAS origin and WebSocket through the official Unix-socket gateway.

Source: [user-no-found/FnTuiOS](https://github.com/user-no-found/FnTuiOS).
Download architecture-specific FPKs from [Releases](https://github.com/user-no-found/FnTuiOS/releases).

This repository contains the gateway, build adapters, reviewed translation
memory and fnOS packaging. Upstream source is fetched into ignored build
storage and is never vendored. Every build resolves the current default-branch
HEAD of `user-no-found/Fnbak-tuios`, as configured by `project.json.upstream`,
records one SHA and uses it for both upstream executables and architectures.
The fork retains the canonical `github.com/Gaurav-Gosain/tuios` Go module and
imports and the original licenses. These identifiers do not choose the Git
repository fetched by this build. A new phrase or dependency asset requires review before
another Chinese package can be built.

Build on native Linux amd64 with Python 3.11+, Git, taskset and official fnpack.
Provide explicit checkouts (or FnPackKit's extracted source distribution) for
the shared tools; each project keeps its own metadata and adapters:

```sh
/path/to/BuildFlow/setup.sh
/path/to/FnPackKit/setup.sh
./setup.sh --playwright /path/to/node_modules/playwright --chromium /path/to/chrome
./build.sh
```

Initialization validates and records already installed paths; it does not install
dependencies. Shared tool paths are stored under the user configuration directory,
and browser paths stay in this checkout's ignored `.build/environment.json`.
After initialization, run `./build.sh` without exports in an existing or new shell.
Explicit environment variables still override the saved paths.

The root script locates this repository from its own path, so it also works
from another working directory. FnPackKit loads the selected application's
plan, and BuildFlow executes source resolution, localization, tests and native
compilation. FnPackKit renders and checks the actual FPK. Both tool versions
must satisfy the requirements in `project.json`; these tools are build-time
dependencies and are not installed as NAS applications. The shared Go runtime
is linked into `fntuios` through a temporary module file. The tool checkouts and
their source hashes are recorded with the build inputs. The tool repositories
must contain their Python package, runtime and shared web asset sources before this command can
work; an empty remote repository is not an installable dependency.

The compiler is the newer requirement from upstream `go` and `toolchain`
directives, verified against go.dev's SHA-256. It stays in
`.cache/toolchains/`. Compilation and tests inherit bounded CPU affinity and
niceness 10. Final FPKs and SHA-256 sidecars are written directly into `output/`.
Logs, package provenance, screenshots and verification reports stay under
`.build/<run-id>/`; `.build/last-success.json` identifies the verified artifacts.
A failure preserves previous successful packages and their pointer.
The shared display shows the current nested action, target and elapsed time,
including quiet scans and packaging work. Captured registry data stays in the
build log; expected negative checks do not mark the whole build as failed.

`project.json` is the source of the application package version.
Interactive builds show the current version and let you select a stable
`x.x.x` or test `x.x.x-xx` build. Staying in the same channel increments the
stable patch or test suffix; switching channels keeps the base version:
`0.0.1` becomes `0.0.1-01`, and `0.0.1-02` becomes `0.0.1`. The selected value
is written back under the shared build lock before the application plan loads.
The installed version is displayed as information; the project's version owns
the default. CI can use `./build.sh --next-version --channel stable`, or select
an exact value with `--build-version 0.0.1`.

This package version is independent of upstream's release version. Each new
build still resolves the default branch's latest HEAD once and records its SHA;
choosing a stable package channel does not switch source selection to releases.
Both architectures share the selected package version and source SHA. Select
architecture targets through `build.architectures`. The installed application
ID remains `tuios` and the display name remains `FnTuiOS`.

Choose the FPK for the device architecture and install it through fnOS App
Center. The runtime requires Bash, flock and PTY support; it does not
require Go or Docker. Click FnTuiOS from the NAS desktop after login. The same
NAS URL is used with FN Connect; actual direct and relay routes depend on the
deployed NAS gateway and must be checked on that device.

Application stop waits for each user's daemon to save sessions before stopping
its web process. Closing the iframe disconnects the browser and keeps its user
instance available. Stopping or upgrading ends running shell jobs; restored
session history does not resume killed processes. Legacy shared `tuios` data
is retained in place and is not copied into users' sessions.

See [runtime configuration and security](docs/runtime.md),
[localization review](docs/localization.md) and [build verification](docs/build.md).
