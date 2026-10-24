# Third-party notices

The Things schema relationships, packed date and reminder decoding,
and list membership rules in `internal/readstore` and `internal/service`
are adapted from [things.py](https://github.com/thingsapi/things.py),
by its authors and contributors, under the Apache License, Version 2.0.
This project translates those rules into Go and adds schema checks,
filtering, transaction handling, ordering, and error handling.
The full license text is in `licenses/things.py-Apache-2.0.txt`.

The Go standard library and the pinned Go modules keep their upstream licenses.
The `licenses` directory contains their license texts and notices,
including the third-party notices supplied by modernc.org/sqlite and
modernc.org/libc.
`licenses/manifest.json` records the SHA-256 digest of each license file
and this notice.
