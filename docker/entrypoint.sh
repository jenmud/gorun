#!/bin/sh
# Starts the web service. GORUN_FILE, when set, is the Gorunfile to load; without one the
# service uses the one kept in its history database, or with -live shows only the live and
# history pages. Arguments are passed on to gorun -serve.
set -eu
if [ -n "${GORUN_FILE:-}" ]; then
	set -- "$GORUN_FILE" "$@"
fi
exec gorun -serve "$@"
