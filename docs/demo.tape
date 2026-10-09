# Record docs/demo.gif with `make demo`.
# The demo reads a synthetic library from docs/demo.sql
# and uses --dry-run for writes, so it never reads or changes your Things data.

Output docs/demo.gif

Require jq
Require sqlite3

Set Shell bash
Set FontSize 16
Set Width 1280
Set Height 320
Set Padding 24
Set Theme "Catppuccin Mocha"
Set TypingSpeed 35ms

Hide
Type@1ms `export PS1='\[\e[1;35m\]$\[\e[0m\] ' DEMO="$(mktemp -d)" BIN="$PWD/things"` Enter
Type@1ms `sqlite3 "$DEMO/main.sqlite" < internal/testutil/testdata/v24.sql && sqlite3 "$DEMO/main.sqlite" < docs/demo.sql` Enter
Type@1ms `things() { "$BIN" --database "$DEMO/main.sqlite" --config-dir "$DEMO" "$@"; }` Enter
Type@1ms "clear" Enter
Show

Type "things list today" Sleep 400ms Enter
Sleep 3s

Ctrl+L
Type "things get Wr3" Sleep 400ms Enter
Sleep 4s

Ctrl+L
Type "things search launch --json | jq -c '.data[] | {title, id}'" Sleep 400ms Enter
Sleep 3s

Ctrl+L
Type `things add "Draft release notes" --project "Website refresh" --when tomorrow --dry-run --json | jq .data.request.patch` Sleep 400ms Enter
Sleep 4s

Ctrl+L
Type "things complete Rv7 --dry-run --json | jq -c .data.request" Sleep 400ms Enter
Sleep 4s

Hide
Type@1ms `rm -rf "$DEMO"` Enter
