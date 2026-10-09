# Build and verification

Run the root `build.sh` on native Linux after the one-time `setup.sh` commands
in the README. The application entry reads the shared generated
`${XDG_CONFIG_HOME:-$HOME/.config}/buildflow/environment.sh`; the entry itself loads this file, so no new terminal is required. Explicit `BUILDFLOW_PATH` and
`FNPACKKIT_PATH` variables override recorded defaults. FnPackKit's root
may also be an extracted source distribution containing `runtime/go.mod`;
the Python wheel alone does not contain the Go runtime. The tools are separate
repositories with no application registry. `project.json` owns this application's
identity, architectures, tool requirements and adapter declaration.

The application entry and `FnPackKit/build.sh --project /path/to/FnTuiOS/project.json`
use the same plan and execution engine. BuildFlow's generic CLI does not execute
FPK adapters; select FnPackKit for this project. Discovery reads declarations
without importing application code. FnTuiOS has no native Windows FPK entry.

The project version is selected and written back by BuildFlow under its build
lock, then FnPackKit loads the application plan with that selected value. Use
`--build-version 0.0.1` for an exact noninteractive choice or `--next-version
--channel stable` for the next stable version. Channel changes retain the base
version. Project metadata owns the default; the installed NAS version is only
shown for reference. Package filenames, manifests and binary stamps consume the
same selected value.

The plan resolves `project.json.upstream` (`user-no-found/Fnbak-tuios`)'s default
branch HEAD once and fetches that exact SHA through
BuildFlow, obtains the verified Go compiler selected from both `go` and `toolchain` directives, and makes a private sip copy.
Localization review and regression gates precede compilation. Application
steps use the common runner; FnPackKit owns rendering, fnpack and validation of
the actual package archive. The application supplies its own templates,
translated resources, image adapters and binary requirements.

For a source review completed immediately before a build, an explicit
`FNTUIOS_RESOLVED_SOURCE=/absolute/path/resolved.json` can reuse that recorded
resolution. The record must include the matching fork repository/URL, full SHA and
resolution time. This mode records its supplied input and never claims to have
queried HEAD again. Without this explicit input, each build resolves current
HEAD; network failures do not silently select an old cache. Records for the
official repository or any other source are rejected, and Git caches are keyed
by the configured repository URL. The packaging repository's Git remote and the
fork's canonical Go module/import paths are independent of this source selection.

Each build creates private `/tmp/fntuios-<random>` storage, mode 0700, and
supplies it to native commands as both `TMPDIR` and `GOTMPDIR`. This overrides a
terminal's inherited NAS runtime directory, which may be mounted `noexec`.
The build checks actual execution and rejects a Git ancestor, keeping temporary
test repositories independent from the application checkout. `/tmp` must allow
native execution; failure is reported without changing mount permissions.
Ownership and cleanup status are recorded in `.build/<run-id>/tmp/owner.json`;
the owned temporary tree is removed on normal exit and handled failures, while
logs stay in the work directory. Independent package verification uses the same
policy and records its workspace under `.build/`.

Work stays in `.build/<run-id>/`: `build.log`, `events.jsonl`, `resolved.json`,
`reports/` and the disposable upstream/sip trees. The application post-package hook runs native worker and Chromium checks before
publication. Reports include localization
coverage, candidate classifications, negative controls and source patch.
`output/` contains final FPKs and their checksum sidecars directly. BuildFlow's
`report.json`, FnPackKit package records and application evidence stay in the
corresponding `.build/<run-id>/` workspace. Only a fully verified run replaces
`.build/last-success.json` (schema 2), which lists flat artifact names and hashes
and points to that workspace's report. Older output directories are left intact;
the current entry does not read old success-record schemas.

Each FPK contains `app/build.json` with upstream SHA, compiler identity,
application input digest, tool/runtime source hashes, localization coverage and
binary hashes. FnPackKit's `.build.json` remains inside the build workspace and adds archive
verification to this record under `application`. A temporary Go modfile supplies the local
runtime replacement; maintained `go.mod` has no machine-specific absolute path.
Both architectures use the same application inputs and resolved upstream SHA.

The application's `setup.sh --playwright PATH --chromium PATH` checks the existing
Playwright Node package and Chromium `--version`, then atomically saves the two
paths to ignored `.build/environment.json`. It preserves the old configuration
if validation fails. Omitted options reuse existing configuration or explicit
environment settings; no dependencies are downloaded. Run setup again after
moving a browser installation.

Application plan selection loads the saved browser paths for either the local
entry or the shared FnPackKit entry. Explicit `FNTUIOS_PLAYWRIGHT` and
`FNTUIOS_CHROMIUM` variables take precedence, including invalid values that must
fail rather than fall back. No application paths enter the shared tool registry.
The one-command build verifies its candidate FPK
before updating the success pointer. To repeat verification of the latest
successful amd64 package:

```sh
export PYTHONPATH="$BUILDFLOW_PATH/src:$FNPACKKIT_PATH/src"
python3 scripts/verify_latest.py --playwright /path/to/playwright --chromium /path/to/chromium
```

The verifier runs as an ordinary user. It safely extracts the final FPK, checks
ELF architecture and hashes, executes the packaged version/configuration
commands, and uses the actual packaged worker for daemon/web startup and saved
session restart. Its Chromium fixture reaches the web service through a
peer-verified Unix gateway, using a simulated broker and the current real UID.
Every HOME/XDG/runtime path belongs to a temporary fixture. Browser checks cover
Chinese iframe screens, actual terminal input, clipboard text/images, pane and
session authority, bounded uploads and token replay. The keyboard negative
control removes the served paste hook and requires the text regression to fail.
Evidence stays in `.build/verification/<fpk-hash>/`, bound to the actual FPK hash.

These ordinary-user checks do not execute the privileged broker, switch between
two real UIDs, run installation/upgrade callbacks, or establish actual FN
Connect login and entitlement behavior. Their report lists these boundaries.
ARM64 receives compilation, ELF and matching-input verification, without host
execution. Native temporary directories are not a security sandbox. No Docker
or substitute container is used for building, verification or deployment.

An administrator can explicitly test ownership restoration with the packaged
binary and template in an isolated native fixture:

```sh
python3 scripts/verify_state_preparation.py \
  --binary /path/to/unpacked/app/bin/fntuios \
  --template /path/to/unpacked/app/etc/gateway.json --uid 1000
```

This command requires a root shell and an existing non-system NSS user; it does
not create accounts or use the installed application's paths. It creates its
own `/tmp/fntuios-state-proof-*` directory, checks active-supervisor refusal,
hardlink rejection, symlink isolation, nested ownership, preserved modes and
bytes, idempotence, and real-user access. It removes fixture data and retains
`verified.json` at the printed path. It never runs package callbacks, whose
production runtime path is `/run/fntuios`, and it is not run by the ordinary-user
build. Its explicit result is separate from native worker persistence and
browser evidence.

After verification, copy packages with:

```sh
python3 scripts/deliver.py --verified /path/to/verified.json \
  --copy-to /path/to/destination --arm64 /path/to/matching_arm64.fpk
```

Delivery rechecks hashes and common inputs. It does not install, stop or replace
an application. The CI workflow uses the same root build entry and native
verification before artifact upload. Source changes require a new input review;
no job automatically approves translations or publishes a release.

The CI bootstrap pins fnpack 1.2.3 with its published download checksum. Local
build provenance records the actual invoked fnpack version. The workflow requires
the shared repositories to be published before remote checkout can succeed.

To check initialization independently of a full TUIOS build, run
`scripts/verify_setup.py --buildflow /path/to/BuildFlow --fnpackkit /path/to/FnPackKit
--playwright /path/to/playwright --chromium /path/to/chrome --output /path/to/evidence`.
It uses an isolated HOME and temporary application, builds a small real FPK,
and loads the real application plan without fetching or compiling TUIOS.

Localization negative-control details are always written to
`reports/localization-checks/negative-controls.json`. Library calls stay quiet
while the shared engine displays the active stage, nested action, target and
elapsed time. Scanning, translation reconciliation, inventory checks, each
architecture/binary compilation and FPK staging expose semantic actions through
the shared context. Captured JSON remains in logs. The standalone
`verify_localization.py --verbose` option prints the full report when requested;
normal failures retain their actual diagnostic and exit status.
