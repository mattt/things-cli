# Release checks

On 2026-10-08, the synthetic 10,000-item read benchmark took about 38 ms
on an Apple M4 Max with Go 1.26.2 (Darwin arm64).
The separate CLI benchmark took about 50 ms, including process launch,
database discovery through an explicit path, the read, and JSON output.
These are warm filesystem-cache measurements from `make benchmark`.
They do not establish performance on a fresh Mac or a live Things library.

The automated tests use synthetic databases.
Complete the following manual checks before publishing a release.
Use disposable tasks and a test Things library.

| Check | Required evidence |
| --- | --- |
| Things lists | Compare membership and ordering for Inbox, Today, Evening, Upcoming, Anytime, Someday, Logbook, and Trash against Things itself. Include scheduled and Someday project children. |
| Schema versions | Inspect sanitized v22 and v23 fixtures; the current generated fixture models v24. Do not claim validation of every accepted version from a v24 fixture alone. |
| Calendar and reminders | Check local midnight, DST transitions, Evening, clearing scheduling and reminders, and unchanged reminders when another field changes. |
| Individual callbacks | Use disposable tasks to establish add, add-project, update, rejection, and cancel behavior through Launch Services. |
| JSON callbacks | Verify top-level IDs and nested project read-back; JSON does not return nested IDs. |
| Concurrent edits | Change an affected field during dispatch and document the limits of fingerprint checks. |
| Keychain | Check the hidden prompt, locked Keychain, access denial, token deletion, and environment precedence. |
| Permissions | Check privacy denial and a read-only WAL library whose sidecar files cannot be created. |
| Identifier collisions | Check that the callback URL scheme resolves to `co.dododo.things-cli.callback` before release. |
| Packaged helper | Sign with Developer ID, notarize, staple, and verify Gatekeeper behavior. Ad hoc signing establishes only a local development build. |
| Standalone distribution | Evaluate standalone signing and notarization, then install both archives on fresh Intel and Apple silicon Macs without developer runtimes. |
| Homebrew | Install the formula that GoReleaser publishes to `mattt/homebrew-tap` and run its version check on both architectures. |
| Performance | Run the 10,000-item benchmark on a named reference Mac and measure process cold-start separately. The read target is 500 ms. |

The read model derives list membership from things.py and adds parent-context
filters and stable ID ordering.
These rules have synthetic coverage, but manual Things parity is a separate gate.
Future recurring occurrences are never generated as persisted tasks.

The callback bridge has a separate native transport check.
Set `THINGS_TEST_HELPER` to an absolute built app path to run it.
The check sends only a synthetic callback to this project's app,
and does not send a Things mutation.

```sh
THINGS_TEST_HELPER="$PWD/dist/Things CLI Callback.app" \
  go test ./internal/callback -run TestNativeHelperDelivery -v
```

## Archive licenses

`make licenses` copies the license and notice files from the cached Go modules
linked into the Darwin amd64 and arm64 binaries, plus the Go license.
It uses no network access.
The things.py Apache license is kept in `licenses/things.py-Apache-2.0.txt`.
Run `make licenses` after a dependency or notice change and commit the result.
`make test` compares the copies with the module cache and checks the manifest.
The GoReleaser before hook also checks the copies before building an archive.

The earlier untracked `dist/dependency-licenses` directory in the main checkout
contained 28 files that match the pinned module cache.
There was no collection step in the committed build configuration.
The checked-in `licenses/dependencies` directory replaces that build dependency.
The things.py license text was recovered from the earlier local release archive;
the source revision and any upstream NOTICE file still need an upstream check
before publication.

After a snapshot or release build, check each archive with:

```sh
make check-release ARCHIVES="/path/to/things_darwin_arm64.tar.gz /path/to/things_darwin_amd64.tar.gz"
```

The check requires the binary, README, project license, adaptation notice,
license manifest, and every license file with its recorded digest.
It rejects missing, changed, or duplicate license entries.
