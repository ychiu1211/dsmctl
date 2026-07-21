#!/usr/bin/env bash
# Fail if real internal-lab data has leaked into tracked source.
#
# This repository is public. Fixtures and docs must never contain the internal
# test lab's real details — use RFC 5737 documentation IPs
# (192.0.2.x / 198.51.100.x / 203.0.113.x) and generic names (test-nas / testuser).
# Sessions developing against the lab tend to bake real hostnames/IPs into test
# fixtures; this scanner (run in CI and via the pre-commit hook) stops that.
#
# High-signal markers of the real internal lab (extend as new leaks appear):
#   10.17.x        — the real internal /16 range
#   Derek / deryck — the real device-owner name / username
#
# Usage: scripts/check-no-lab-data.sh        (scan the whole tracked tree)
set -euo pipefail

pattern='10\.17\.[0-9]|[Dd]erek|deryck'

# Scan tracked .go and .md, excluding this script (which necessarily names the patterns).
if hits=$(git grep -nIE "$pattern" -- '*.go' '*.md' ':!scripts/check-no-lab-data.sh' 2>/dev/null); then
	echo "ERROR: real internal-lab data found in tracked source." >&2
	echo "Replace with RFC 5737 documentation IPs (192.0.2 / 198.51.100 / 203.0.113)" >&2
	echo "and generic names (test-nas / testuser). Matches:" >&2
	echo "$hits" >&2
	exit 1
fi
echo "OK: no real internal-lab data markers in tracked source."
