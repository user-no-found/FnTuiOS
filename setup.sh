#!/bin/sh
set -eu
project_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
config="${XDG_CONFIG_HOME:-$HOME/.config}/buildflow/environment.sh"
if [ -f "$config" ]; then . "$config"; fi
: "${BUILDFLOW_PATH:?Run BuildFlow setup.sh to register the shared tools}"
export PYTHONPATH="$BUILDFLOW_PATH/src${PYTHONPATH:+:$PYTHONPATH}"
exec "${PYTHON:-python3}" "$project_dir/scripts/setup.py" "$@"
