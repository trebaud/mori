#!/bin/sh
# gh stub: prints a single canned open PR for any branch.
cat <<'JSON'
[{"number":42,"state":"OPEN","isDraft":false,"url":"https://github.com/example/repo/pull/42","title":"stub pr"}]
JSON
