#!/bin/bash
# Runs the checker in the background as a systemd service. It keeps running
# after you log out, starts again after a reboot, and resumes where it stopped.
#
# Usage, as root, in the folder of the checker binary:
#   bash service.sh install <list file> [rate]       check a list in the background (default rate: 20/s)
#   bash service.sh install <results folder> [rate]  continue a run folder, e.g. one made by "checker merge"
#   bash service.sh recheck <categories> [rate]      check again some results, e.g. dns-timeout,connect-failed
#   bash service.sh status                           show the progress
#   bash service.sh logs                             follow the log (Ctrl+C to leave, the check goes on)
#   bash service.sh stop                             stop, the running checks are saved
#   bash service.sh start                            continue where it stopped
#   bash service.sh uninstall                        remove the service, the results are kept
set -euo pipefail

NAME=domain-checker
UNIT=/etc/systemd/system/$NAME.service
DIR=$(cd "$(dirname "$0")" && pwd)
BIN=$DIR/checker

usage() {
    sed -n '2,14p' "$0" | sed 's/^# \{0,1\}//'
    exit 1
}

fail() {
    echo "error: $*" >&2
    exit 1
}

need_root() {
    [[ $EUID -eq 0 ]] || fail "run as root"
    command -v systemctl >/dev/null || fail "systemd is not available on this server"
}

need_binary() {
    [[ -f $BIN ]] || fail "checker binary not found: $BIN"
    # Archives made on Windows have no executable bit, are writable by every user
    # and owned by the user who built them, but the service runs the binary as root
    chown root:root "$BIN" "$DIR/service.sh"
    chmod 755 "$BIN" "$DIR/service.sh"
}

check_rate() {
    [[ $1 =~ ^[0-9]+$ && $1 -gt 0 ]] || fail "rate must be a number above 0"
}

not_running() {
    if systemctl is-active --quiet "$NAME"; then
        fail "a check is already running, stop it first: bash $0 stop"
    fi
}

# results_dir prints the results folder of the installed service
results_dir() {
    [[ -f $UNIT ]] || fail "the service is not installed, run: bash $0 install <list file>"
    sed -n 's/^# results: //p' "$UNIT"
}

# json_value <file> <key> prints a number or string value of run.json,
# or nothing if the key is missing
json_value() {
    tr -d '\n' < "$1" | { grep -o "\"$2\": *[^,}]*" || true; } | head -1 | sed 's/^[^:]*: *//; s/"//g'
}

# write_unit <results folder> <description> <checker arguments...>
write_unit() {
    local results=$1 description=$2
    shift 2

    cat > "$UNIT" <<EOF
# Created by $DIR/service.sh
# results: $results
[Unit]
Description=Iran blocked domains checker: $description
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=$DIR
ExecStart="$BIN" scan -dir "$results" $* -progress 1m
Restart=on-failure
RestartSec=60

# Lower priority than the other services of the server
Nice=15
CPUWeight=20
IOSchedulingClass=idle
MemoryMax=400M

# Like Ctrl+C: the running checks finish and are saved. Writing the text
# lists of a list of 10 million domains takes about 2 minutes.
KillSignal=SIGINT
TimeoutStopSec=300

[Install]
WantedBy=multi-user.target
EOF

    systemctl daemon-reload
    systemctl enable "$NAME" >/dev/null 2>&1
    systemctl restart "$NAME"
}

install() {
    need_root
    need_binary
    local target=${1:-} rate=${2:-20}
    [[ -n $target ]] || usage
    check_rate "$rate"
    not_running

    local results
    if [[ -d $target ]]; then
        [[ -f $target/checker.db ]] || fail "$target is not a run folder: it has no checker.db"
        results=$(realpath "$target")
        write_unit "$results" "$(basename "$results")" -rate "$rate"
    else
        [[ -f $target ]] || fail "list file not found: $target"
        local list name
        list=$(realpath "$target")
        name=$(basename "$list")
        results=$DIR/results/${name%.*}
        write_unit "$results" "$name" -i "\"$list\"" -rate "$rate"
    fi

    echo "Started checking at $rate domains per second."
    echo "Results: $results"
    echo
    echo "  bash $0 status    show the progress"
    echo "  bash $0 logs      follow the log"
    echo "  bash $0 stop      stop, continue later with: bash $0 start"
}

recheck() {
    need_root
    need_binary
    local categories=${1:-} rate=${2:-20}
    [[ -n $categories ]] || usage
    check_rate "$rate"
    not_running

    local results since
    results=$(results_dir)
    # A fixed start time, so the service continues this recheck after a reboot
    # instead of starting it again
    since=$(date -u +%Y-%m-%dT%H:%M:%SZ)
    write_unit "$results" "recheck $categories" -recheck "$categories" -since "$since" -rate "$rate"

    echo "Started checking again: $categories, at $rate domains per second."
    echo "See the progress with: bash $0 status"
}

status() {
    local results meta
    results=$(results_dir)
    meta=$results/run.json

    echo "Service: $(systemctl is-active "$NAME" 2>/dev/null || true)"
    echo "Results: $results"
    if [[ ! -f $meta ]]; then
        echo "Not started yet"
        return
    fi

    local checked total state rate selected rechecked
    checked=$(json_value "$meta" checked)
    total=$(json_value "$meta" total)
    state=$(json_value "$meta" state)
    rate=$(json_value "$meta" rate)
    selected=$(json_value "$meta" selected)
    rechecked=$(json_value "$meta" rechecked)
    awk -v c="$checked" -v t="$total" -v r="$rate" -v s="$state" -v sel="$selected" -v re="$rechecked" 'BEGIN {
        printf "State:   %s\n", s
        printf "Checked: %d of %d (%.1f%%)\n", c, t, t ? c * 100 / t : 0
        left = t - c
        if (sel != "") {
            printf "Recheck: %d of %d\n", re, sel
            left = sel - re
        }
        if (s != "completed" && r > 0) {
            left = left / r
            printf "Left:    about %dh %dm at %d per second\n", left / 3600, (left % 3600) / 60, r
        }
    }'

    echo
    echo "Results by category:"
    tr -d '\n' < "$meta" | grep -o '"counts": *{[^}]*}' | grep -o '"[a-z-]*": *[0-9][0-9]*' |
        sed 's/"//g; s/: */ /' | awk '{ printf "  %-16s %d\n", $1, $2 }'

    echo
    echo "Last log lines:"
    journalctl -u "$NAME" -n 3 --no-pager -o cat 2>/dev/null | sed 's/^/  /' || true
}

case ${1:-} in
install)
    shift
    install "$@"
    ;;
recheck)
    shift
    recheck "$@"
    ;;
status)
    status
    ;;
logs)
    journalctl -u "$NAME" -f -o short-iso
    ;;
stop)
    need_root
    systemctl stop "$NAME"
    echo "Stopped. Continue with: bash $0 start"
    ;;
start)
    need_root
    results_dir >/dev/null
    systemctl start "$NAME"
    echo "Started. See the progress with: bash $0 status"
    ;;
uninstall)
    need_root
    systemctl disable --now "$NAME" >/dev/null 2>&1 || true
    rm -f "$UNIT"
    systemctl daemon-reload
    echo "Service removed. The results are kept in $DIR/results"
    ;;
*)
    usage
    ;;
esac
