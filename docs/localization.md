# Localization inputs and release checks

`locales/memory/*.json` stores reviewed Simplified Chinese translation memory.
Each record contains the exact English `source`, Chinese `translation`, a
`scope`, and a `reason`. A record with `decision: "keep"` requires a specific
reason explaining a machine value, shortcut, example, proper name or other
content that must remain intact. `locales/glossary.json` defines terminology.
Memory is retained when a source message is removed; inactive entries do not
count toward current coverage.

The scanner records Go string literals by file, declaration, original hash and
occurrence, with AST context. Imports, serialization tags, map/index keys,
comparison/switch discriminators and known machine arguments are protected.
Native display packages are classified through reviewed memory. The scanner
also inventories literals outside those packages, embedded-resource directives
and runtime assets; changes require an explicit review even when they are not
yet identified as a display entry point.

Repeated literals within a declaration also require checking their AST context:
inserting an earlier occurrence can move a display word's ordinal ID onto an
internal argument. Cockpit count labels have separate records from glyph-state
keys. The display contract checks that projection translates the labels while
retaining the exact state arguments passed to the glyph lookup.

Settings labels, descriptions and accepted choices use generated display maps.
Original option paths, enum values and registry descriptions used by JSON or
configuration remain unchanged. The help renderer projects action and scoped
binding descriptions at display time. Other native translations replace only
exact source literal spans resolved from the current AST. User terminal output,
commands, file contents and external program interfaces are not rewritten.

The preset picker summaries are extracted through the typed preset API,
including summaries stored in embedded TOML files. Their translations apply
only when rendering the picker; preset names, bindings and source files remain
canonical. Preset help intended for the command line is outside this display
projection and is covered by the source asset inventory.

Built-in risk rule names and explanations are reviewed as registry text. The
Inbox uses a separate display map; the original rule identifiers, matching,
acknowledgement values and JSON remain unchanged. Custom rule names are shown
as supplied. Native contracts exercise the actual Inbox lines and require the
original display path to fail the translation assertions.

Configuration-backed native displays use the `config-display` classification.
The scanner follows the declared prefix-menu, keyboard-report, scope and
configuration-warning display APIs through their local functions, methods and
typed display tables, including helpers moved into another source file.
Package-level menu tables include keyed descriptions and titles, and the second
positional field of `menuPart`; its first field remains the canonical action.
Display fields and helper parameters are distinguished from actions, chords,
process names and configuration keys. Formatting wrappers retain their actual
format-argument index, so placeholder checks also apply to helper calls.

The build generates separate localized API and table copies from the reviewed
source. It substitutes display literals before formatting, preserving dynamic
counts and interpolated values. Routing constructors and binding caches stay
canonical. Native renderers explicitly call the display APIs; the original
configuration APIs and tables remain unchanged. The projection report records
each translated context and requires it to be applied exactly once. Missing
catalog entries, changed source classifications, unresolved ambiguous methods
and detected writes to shared state stop generation. Receiver resolution follows declared return slots, including
grouped named results and tuple assignments. Unknown receivers remain subject
to the ambiguity rejection. Native contract checks
compare original API results before and after using the localized APIs.

An explicit custom sidebar title and a custom command description remain user
data even if their text matches a built-in label. Preformatted
`UserConfig.LoadWarnings` messages from loading, includes and reload history
are outside this display-source projection; newly computed validation messages
use the localized validation API. Existing terminal/session titles and output
from external programs keep their original meaning and are not globally
replaced. These boundaries are reported separately from reviewed UI coverage.

The sip dependency is copied into a build-local directory. Its HTML text and
accessible labels and known JavaScript display contexts are translated before
compilation. Complete Go, HTML, CSS and JavaScript asset hashes require review
on dependency changes. A bounded adapter forces WebSocket and retains the
same-origin relative URL behavior needed by the NAS gateway.

Use the extracted source and sip directory from a failed build to refresh the
candidate review:

```sh
python3 scripts/localize.py --source /path/to/upstream --sip /path/to/sip \
  --go /path/to/go --output dist/review --refresh-uncovered
```

`uncovered-sources.json` groups missing source strings with their contexts;
`unclassified.json` preserves each individual semantic record. Generic scopes
are `native-ui`, `registry` and `browser`. A source file prefix or exact semantic
ID can override generic memory when the same word has different meanings.
A reviewed record may only be applied in an eligible display context.

After reviewing source classifications, adding translations and checking the
adapters, record the reviewed inventory explicitly:

```sh
python3 scripts/localize.py --source /path/to/upstream --sip /path/to/sip \
  --go /path/to/go --approve-inventory
```

This command requires every current display candidate to be classified. The
release builder never approves inventories. It rejects missing/ambiguous
translations, source/context drift, invalid format or template parameters,
changed dependency assets and missing/non-unique adapter targets. It writes
expanded records containing source/hash/context and a coverage report beside
the package, preserving the independent memory files.

The covered entry points and retained exceptions are measurable; static
analysis cannot prove arbitrary future architectures or all external errors
are fully localized. Upstream structural changes may need a new adapter.
Chinese rendering and hit targets must also be checked at real terminal widths.
