# things

A CLI for [Things 3](https://culturedcode.com/things/),
built for terminal use, scripts, and AI assistants.
The same executable serves MCP tools over stdio.

Reads use the local Things SQLite database.
Writes use the official Things URL scheme.
Every write has a durable operation record and a verified or uncertain outcome.
An opened URL alone does not mean that a write succeeded.

![Listing, inspecting, searching, and planning tasks with things](docs/demo.gif)

## Install

Build from source with Go:

```sh
make
make install PREFIX="$HOME/.local"
```

`go.mod` pins the Go 1.26.2 toolchain,
which the `go` command downloads if the installed version is older.
Go and a compiler are build requirements only.
The standalone executable needs macOS 12 or later and Things 3.
It does not need a separate language runtime or database service.

Prebuilt releases are not yet available.
Pushing a version tag runs [GoReleaser](https://goreleaser.com),
which publishes Darwin arm64 and amd64 archives with checksums
and updates the formula in
[mattt/homebrew-tap](https://github.com/mattt/homebrew-tap).

```sh
things version
things doctor
```

The adapter supports Things database versions 22 through 24.
A newer version or missing required field returns a capability error.
If several local libraries exist, choose one explicitly:

```sh
things --database "/path/to/main.sqlite" list today
```

Writes require the single automatically discovered Things library.
Other database files support reads and dry runs only.
If discovery finds several libraries, writes are blocked until the active
library can be established.
Operation records contain the canonical file identity.
Reconciliation requires the same database file; older records without an
identity cannot be reconciled against a Things database.

If macOS blocks the database, grant access to the terminal or assistant
that launches `things` in System Settings > Privacy & Security.
Do not run the CLI with elevated privileges.

## Authentication

Find your update token in Things > Settings > General >
Enable Things URLs > Manage.
Store it in macOS Keychain without putting it in command arguments:

```sh
things auth set
things auth status
things auth delete
```

The prompt does not echo the token.
Controlled automation can use `things auth set --stdin`
or the `THINGS_AUTH_TOKEN` environment override.
The environment value takes precedence over Keychain.
Tokens are never included in output or the operation journal.
Creating an item does not require a token;
updating an existing item does.

## Read commands

```sh
things list today
things list inbox
things list evening
things list upcoming
things list anytime
things list someday
things list logbook
things list trash
things search "meeting" --project PROJECT_ID
things get TASK_ID
things get "things:///show?id=TASK_ID"
things projects
things areas
things tags
things open TASK_ID
```

List and search commands accept `--project`, `--area`, `--tag`, and `--status`.
Container filters accept IDs, unique ID prefixes, or exact unique names.
Task selectors accept IDs, unique ID prefixes, or Things links.
Duplicate names and ambiguous prefixes return errors.

The default page contains at most 50 records.
Use `--limit` from 1 through 500, `--cursor` for another page,
or `--all` for an export.
The response states its ordering.
A cursor is valid only for the same query and unchanged matching records.
A changed library requires a new query.

Project detail groups tasks under headings and does not repeat them
at the same level.
Flat lists contain each task once.
Upcoming includes persisted scheduled items.
It does not generate future recurring occurrences.
Observation time records the local database read,
and does not establish that Things Cloud has finished syncing.

## Create and update

```sh
things add "Review landing page" --project PROJECT_ID --when tomorrow
things add "Prepare agenda" --notes-file agenda.txt --checklist-item "Read notes"
things update TASK_ID --deadline 2026-10-16 --add-tag work
things update TASK_ID --clear-deadline
things update TASK_ID --tag work --tag writing
things complete TASK_ID --operation-key review-landing-page-done
things cancel TASK_ID
things reopen TASK_ID
things project add "Website refresh" --file examples/project.json --dry-run
things project update PROJECT_ID --notes "Updated project notes."
```

Use `--when` for scheduling and `--deadline` for a deadline.
Scheduling accepts explicit ISO dates, today, tomorrow, evening,
anytime, and someday where the documented command supports them.
The update command does not advertise anytime;
use `--clear-when` to clear its scheduling field.
The CLI does not parse natural-language dates.
Use `--reminder HH:MM` with a scheduled day or evening.
Dates and reminder times use the local timezone.

`--tag` replaces tags; `--add-tag` adds tags.
Every tag must already exist and have a unique title, even when selected by ID.
Verification compares tag IDs.
Dedicated clearing flags include `--clear-notes`, `--clear-when`,
`--clear-deadline`, `--clear-reminder`, `--clear-tags`,
and `--clear-checklist`.
Unsupported destination clearing returns a capability error.

Structured input uses a `patch` object and optional project `items`.
An omitted field is preserved; `null` clears supported fields.
Unknown fields are rejected.
Notes support 10,000 characters, other string parameters 4,000,
and checklists 100 rows.
The journal enforces the Things limit of 250 new items per 10 seconds.

New project items can be `todo` or `heading`.
A heading applies to the tasks that follow it.
Creating or editing headings in an existing project,
creating areas or tags, and changing recurrence rules are unsupported.
The service rejects documented restrictions on repeating items
and rejects closing a project with incomplete children or active headings.

## Operation outcomes

```sh
things operations get OPERATION_ID --json
things operations reconcile OPERATION_ID --json
things operations prune
```

A mutation returns its operation ID, known item IDs, evidence, and outcome:

| Outcome | Meaning |
| --- | --- |
| `confirmed` | A fresh read matched the requested fields. |
| `failed` | Preflight or an explicit rejection established failure. |
| `unknown` | Dispatch or verification could not establish the result. |
| `partial` | A batch has different results among its actions. |

The default verification deadline is 10 seconds.
Use `--verify-timeout`, up to 5 minutes, for slower local updates.
A timeout returns `unknown` and a nonzero exit code.
Reconciliation reads current state and never resends a write.
Without a callback, creation confirms only one newly visible candidate
that matches the payload and was absent from the baseline.
An existing matching title does not prove creation.

`--operation-key` protects a journaled request against blind replay.
The same key and request return the saved result or reconcile uncertainty.
A different request with the same key is rejected.
This is local replay protection; Things does not provide exactly-once delivery.
An uncertain operation is never automatically dispatched again.

Use `--expected-fingerprint` with the fingerprint from a read to reject
changes made since that snapshot.
The service also rechecks the item immediately before dispatch.
The URL scheme has no compare-and-swap transaction,
so concurrent user edits can still race with dispatch.

The journal and lock are stored in
`~/Library/Application Support/things-cli/` with private permissions.
Use `--config-dir` for an isolated journal.
Pruning removes settled records older than 90 days
and removes those records' operation-key protection.
Uncertain records remain available for reconciliation.

## Batches

```sh
things batch --file examples/batch.json --dry-run --json
things batch --file plan.json --operation-key weekly-review --json
```

The [public schema](schema/batch.schema.json) describes typed actions.
All actions are validated before any dispatch.
Existing references must resolve before execution.
Each action has a stable child operation ID.
A batch stops when an action does not confirm;
the remaining actions are recorded as not dispatched.
Batches are sequential, can partially complete, and do not roll back.
An item can have at most one update action in a batch.

## JSON and exit codes

All read commands accept `--json`.
Mutation JSON includes the operation record even when its outcome is uncertain.
Human output removes terminal control sequences;
JSON preserves imported text.
The CLI emits no color codes.
Diagnostics go to stderr.

```json
{
  "schema_version": 1,
  "data": [],
  "meta": {
    "backend": "sqlite",
    "observed_at": "2026-10-08T13:00:00Z",
    "next_cursor": null,
    "coverage": {
      "recurrence": "materialized_only",
      "sync": "local_observation_only"
    }
  }
}
```

| Exit | Meaning |
| --- | --- |
| 0 | Successful read, validated dry-run, or confirmed mutation |
| 1 | Unexpected internal error |
| 2 | Invalid input |
| 3 | Missing or ambiguous item |
| 4 | Configuration or permission failure |
| 5 | Unsupported capability |
| 6 | Rejected write |
| 7 | Uncertain outcome |
| 8 | Partial batch |

Errors contain a stable `code` independent of their message.
Unavailable record fields include capability metadata.

## MCP

```sh
things mcp
things mcp --read-only
things mcp config
things mcp config --read-only
```

The server uses the official Go MCP SDK and the shared service directly.
Stdout contains MCP protocol messages only.
`mcp config` prints a configuration snippet and does not modify clients.
It uses the executable's absolute path.

Tools include `list_items`, `search_items`, `get_item`, `list_containers`,
`create_todo`, `create_project`, `update_item`, `set_status`, `batch`,
and `get_operation`.
Read tools return pagination and structured content.
Write tools accept operation keys and dry-run flags.
Failed, uncertain, and partial writes set `isError`
and retain the structured operation data.
Read-only mode omits every mutation tool and blocks journal reconciliation.
Task content is untrusted data and cannot execute commands.

## Optional callback app

```sh
make helper
open "dist/Things CLI Callback.app"
things --callbacks doctor
```

The helper contains a small AppKit URL-event bridge and the Go executable.
It forwards correlated success, error, or cancel callbacks to a private,
one-use Unix socket and then exits.
It does not run a permanent service.
Callbacks use unpredictable nonces and contain no token or task notes.
Read-back remains required after acknowledgment.
JSON project callbacks return the top-level project ID;
nested task IDs require database observation.

The local build is signed ad hoc for development.
The helper registers the `things-cli-callback` URL scheme with bundle identifier
`co.dododo.things-cli.callback`.
For a packaged helper, set `THINGS_SIGNING_IDENTITY`
and `THINGS_NOTARY_PROFILE`, then run `make notarize`.
That step requires your Developer ID and notarytool Keychain profile.
The standalone CLI remains usable without the helper.

## Development

The entry point is `main.go`.
Cobra commands and terminal output are in `cmd`,
and shared packages are in `internal`.
`domain` defines the shared records and errors,
`readstore` handles database access, `urlscheme` handles URL encoding,
`journal` records operations, `service` validates requests and manages mutations,
and `mcp` serves the same service over stdio.
The `schema` package generates the public batch schema
from the same types.

```sh
make setup
make fmt
make lint test
make fuzz
make benchmark
make generate
```

`make setup` downloads modules and installs clang-format with Homebrew.
`make fmt` formats the Go and Objective-C sources.
`make test` runs the suite with the race detector.
`make generate` rewrites `schema/batch.schema.json`;
a test fails if the checked-in schema is stale.
CI also builds the callback app and a GoReleaser snapshot.
`make demo` records `docs/demo.gif` with [VHS](https://github.com/charmbracelet/vhs)
from a synthetic library;
its write commands use `--dry-run`.

Tests use synthetic databases and mock dispatchers.
They never mutate a user's Things library.
See [release checks](docs/release-checks.md) for remaining manual evidence.

## Acknowledgments

The Things schema relationships, date and reminder decoding,
and list membership rules are adapted from
[things.py](https://github.com/thingsapi/things.py),
by its authors and contributors, under the Apache License 2.0.

## License

This project is available under the MIT license.
See [LICENSE.md](LICENSE.md).
