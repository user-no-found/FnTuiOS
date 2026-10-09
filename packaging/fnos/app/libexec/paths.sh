#!/bin/bash
# Resolve the package manager's deployment paths without reading shell config.
: "${TRIM_APPDEST:?Missing TRIM_APPDEST}"
: "${TRIM_PKGETC:?Missing TRIM_PKGETC}"
: "${TRIM_PKGVAR:?Missing TRIM_PKGVAR}"
fnt_app=$(readlink -f -- "$TRIM_APPDEST")
fnt_etc=$(readlink -f -- "$TRIM_PKGETC")
fnt_var=$(readlink -f -- "$TRIM_PKGVAR")
fnt_state=$fnt_var/fntuios
fnt_run=/run/fntuios
fnt_pidfile=$fnt_run/supervisor.pid
fnt_log=$fnt_state/service.log

fnt_stamp() {
    [[ $1 =~ ^[1-9][0-9]*$ && -r /proc/$1/stat ]] || return 1
    local data
    data=$(cat "/proc/$1/stat") || return 1
    data=${data##*) }
    set -- $data
    printf '%s\n' "${20}"
}

fnt_running() {
    local recorded current exe
    [[ -f $fnt_pidfile && ! -L $fnt_pidfile ]] || return 1
    read -r fnt_pid recorded < "$fnt_pidfile" || return 1
    current=$(fnt_stamp "$fnt_pid") || return 1
    [[ $current == "$recorded" ]] || return 1
    exe=$(readlink "/proc/$fnt_pid/exe") || return 1
    [[ $exe == "$fnt_app/bin/fntuios" ]] || return 1
    kill -0 "$fnt_pid" 2>/dev/null
}

fnt_prepare() {
    [[ $(id -u) == 0 ]] || { echo 'FnTuiOS lifecycle requires root.' >&2; return 1; }
    local path mode
    for path in "$fnt_run" "$fnt_state"; do
        [[ ! -L $path ]] || { echo "Refusing symlink: $path" >&2; return 1; }
        if [[ -e $path ]]; then
            [[ -d $path && $(stat -c %u -- "$path") == 0 ]] || { echo "Untrusted state directory: $path" >&2; return 1; }
            mode=$(stat -c %a -- "$path")
            (( (8#$mode & 0022) == 0 )) || { echo "Writable state directory: $path" >&2; return 1; }
        else
            mkdir -- "$path"
        fi
        chmod 711 -- "$path"
    done
    for path in "$fnt_log" "$fnt_pidfile" "$fnt_run/service.lock"; do
        if [[ -e $path || -L $path ]]; then
            [[ -f $path && ! -L $path && $(stat -c %u -- "$path") == 0 && $(stat -c %h -- "$path") == 1 ]] || {
                echo "Untrusted control file: $path" >&2; return 1;
            }
            mode=$(stat -c %a -- "$path")
            (( (8#$mode & 0022) == 0 )) || { echo "Writable control file: $path" >&2; return 1; }
        fi
    done
    if [[ ! -e $fnt_log ]]; then (set -o noclobber; : > "$fnt_log"); fi
    chmod 600 -- "$fnt_log"
}
