#!/bin/sh
set -eu
project_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
shared_environment="${XDG_CONFIG_HOME:-$HOME/.config}/buildflow/environment.sh"
if [ -f "$shared_environment" ]; then
    . "$shared_environment"
fi
if [ "${BUILDFLOW_PATH+x}" = x ]; then
    test -f "$BUILDFLOW_PATH/src/buildflow/__init__.py" || { echo 'Invalid BUILDFLOW_PATH; run BuildFlow/setup.sh for the installed checkout.' >&2; exit 1; }
    PYTHONPATH="$BUILDFLOW_PATH/src${PYTHONPATH:+:$PYTHONPATH}"
fi
if [ "${FNPACKKIT_PATH+x}" = x ]; then
    test -f "$FNPACKKIT_PATH/src/fnpackkit/__init__.py" || { echo 'Invalid FNPACKKIT_PATH; run FnPackKit/setup.sh for the installed checkout.' >&2; exit 1; }
    PYTHONPATH="$FNPACKKIT_PATH/src${PYTHONPATH:+:$PYTHONPATH}"
fi
export PYTHONPATH
if ! "${PYTHON:-python3}" -c 'import buildflow, fnpackkit' >/dev/null 2>&1; then
    echo 'Build dependencies are unavailable. Run BuildFlow/setup.sh and FnPackKit/setup.sh once, then FnTuiOS/setup.sh --help.' >&2
    exit 1
fi
exec "${PYTHON:-python3}" -m fnpackkit --project "$project_dir/project.json" "$@"
