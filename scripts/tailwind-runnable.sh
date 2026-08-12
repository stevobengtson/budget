#!/usr/bin/env bash
#
# Prints the path of a tailwindcss binary that actually executes on this
# machine, repairing a broken macOS code signature if that is what is wrong.
#
# The Tailwind standalone CLI is a Bun-compiled single-file executable, and Bun
# 1.3.12 writes a truncated code signature into the Mach-O it produces
# (oven-sh/bun#29120). macOS 27 enforces signature validity at exec, so the
# kernel SIGKILLs the process before main() — every invocation exits 137,
# including `--help`. The same artifact ships to the GitHub release and to
# nixpkgs, so switching between them does not help. Re-signing ad hoc does.
#
# The nix store is read-only, so the repair is a signed copy at bin/tailwindcss
# rather than an in-place fix. That is also where a non-devbox checkout
# downloads the CLI, so there is one repaired binary either way.
#
# Usage: tailwind-runnable.sh <candidate>
#
# The candidate is required, and is the Taskfile's TAILWIND: the devbox/nix
# binary inside a devbox shell, ./bin/tailwindcss outside one. Deliberately no
# fallback to `tailwindcss` on PATH — an asdf/rbenv shim from some other project
# answers to that name and would quietly build the css with a different version.
set -euo pipefail

candidate=${1:-}
if [ -z "$candidate" ]; then
	echo "usage: $(basename "$0") <tailwind-binary>" >&2
	exit 2
fi

repaired=$(cd "$(dirname "$0")/.." && pwd)/bin/tailwindcss

# The only check that means anything here is whether the thing runs: a killed
# binary is present, executable, and the right size.
# The inner `bash -c` is what keeps "Killed: 9" out of the build output. That
# line comes from the shell that reaps a job killed by a signal, not from the
# command, so it has to be a shell whose own stderr we can redirect. The
# trailing `exit $?` is load-bearing: without it bash exec's the single command
# and is replaced by it, leaving this script to do the reaping and the printing.
runs() {
	[ -x "$1" ] && bash -c '"$0" --help >/dev/null 2>&1; exit $?' "$1" 2>/dev/null
}

if runs "$candidate"; then
	printf '%s\n' "$candidate"
	exit 0
fi
if runs "$repaired"; then
	printf '%s\n' "$repaired"
	exit 0
fi

# nixpkgs ships tailwindcss as a shell wrapper that execs the real Mach-O. The
# wrapper only sets LD_LIBRARY_PATH, which does nothing on macOS, so the exec
# target can be copied on its own.
real=$candidate
if [ -n "$candidate" ] && [ "$(head -c 2 "$candidate" 2>/dev/null)" = '#!' ]; then
	real=$(sed -n '/^exec /p' "$candidate" | grep -o '"/nix/store/[^"]*"' | tail -1 | tr -d '"')
fi

if [ -z "$real" ] || [ ! -f "$real" ]; then
	echo "tailwind: nothing to repair (candidate=${candidate:-none}); run 'task css' to download the CLI" >&2
	exit 1
fi
# Absolute, because the comparison against $repaired below decides whether this
# is a copy or an in-place re-sign, and outside devbox the candidate is the same
# file spelled relatively ("./bin/tailwindcss"). Getting that wrong deletes the
# source and then copies from it.
real=$(cd "$(dirname "$real")" && pwd)/$(basename "$real")

if [ "$(uname -s)" != Darwin ]; then
	echo "tailwind: $real will not run, and signature repair only applies to macOS" >&2
	exit 1
fi

echo "→ repairing tailwind code signature ($real)" >&2
if [ "$real" != "$repaired" ]; then
	mkdir -p "$(dirname "$repaired")"
	# rm first: cp onto a binary that a `--watch` process is running gets ETXTBSY.
	rm -f "$repaired"
	cp "$real" "$repaired"
	# u+w matters: the nix store copy is read-only, and codesign rewrites in place.
	chmod u+wx "$repaired"
fi
if ! codesign --force --sign - "$repaired" 2>/dev/null; then
	echo "tailwind: codesign failed — install the Xcode command line tools (xcode-select --install)" >&2
	exit 1
fi
if ! runs "$repaired"; then
	echo "tailwind: $repaired still will not run after re-signing; delete it and let 'task css' re-download" >&2
	exit 1
fi

printf '%s\n' "$repaired"
