#!/bin/bash
# Merge multiple Go coverage files by taking the union of covered lines.
# For each source line, if any input file marks it as covered, the output marks it covered.
# Usage: merge-coverage.sh file1.out file2.out [file3.out ...] > merged.out

set -e

if [ $# -lt 2 ]; then
    echo "Usage: $0 file1.out file2.out [file3.out ...]" >&2
    exit 1
fi

# Create a temp file for processing
TEMP=$(mktemp)
trap "rm -f $TEMP" EXIT

# Extract mode from first file
MODE=$(head -1 "$1")
if [[ ! "$MODE" =~ ^mode: ]]; then
    echo "Error: First file doesn't start with mode line" >&2
    exit 1
fi

# Combine all files, skip mode lines, and merge duplicates
# For mode: set, take max count for each unique key (file:range statements)
for f in "$@"; do
    if [ ! -f "$f" ]; then
        echo "Error: File not found: $f" >&2
        exit 1
    fi
    tail -n +2 "$f"
done | awk '
{
    # Format: file:start,end statements count
    key = $1 " " $2
    count = $3
    if (!(key in max) || count > max[key]) {
        max[key] = count
    }
}
END {
    for (key in max) {
        print key, max[key]
    }
}
' | sort > "$TEMP"

# Output merged result
echo "$MODE"
cat "$TEMP"
