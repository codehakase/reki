#!/usr/bin/env zsh
# Capture-hook benchmark harness. Times hook call latency for bench/hook_fg.zsh
# and bench/hook_bg.zsh:
#   1. warm run: N prompt cycles per variant
#   2. cold probe: 20 fresh `zsh -f` processes per variant, first-call latency
#   3. concurrency: 10 parallel zsh -f workers x 30 captures, per-worker p99,
#      attempted rows, and inserted rows in the shared DB
# Usage: bench/run.zsh [N] [fg|bg|both]
# Writes only inside a mktemp tree (or $REKI_KEEP=1 to keep it). Setup and
# capture-probe failures exit nonzero.

emulate -L zsh
setopt nobgnice
zmodload zsh/datetime

typeset -g N="${1:-1000}" VARIANT="${2:-both}"
[[ "$N" == <-> ]] || N=1000
case "$VARIANT" in fg|bg|both) ;; *) VARIANT=both ;; esac

typeset -g BENCH_DIR="${0:A:h}" REPO_ROOT="${0:A:h:h}"
typeset -g REKI_BIN="${REKI_BIN:-}"
if [[ -z "$REKI_BIN" || ! -e "$REKI_BIN" ]]; then
  REKI_BIN="${REPO_ROOT}/bin/reki"
fi

typeset -g WORK
WORK="$(mktemp -d "${TMPDIR:-/tmp}/reki-bench.XXXXXX")" \
  || { WORK="/tmp/reki-bench.$$.$RANDOM"; rm -rf "$WORK"; mkdir -p "$WORK"; }
export XDG_DATA_HOME="$WORK/data" XDG_CONFIG_HOME="$WORK/conf" ZDOTDIR="$WORK/zsh"
mkdir -p "$XDG_DATA_HOME" "$XDG_CONFIG_HOME" "$ZDOTDIR" "$WORK/logs"
chmod 700 "$XDG_DATA_HOME" "$XDG_CONFIG_HOME" "$ZDOTDIR" 2>/dev/null

typeset -g DB="${XDG_DATA_HOME}/reki/history.db"
typeset -gi T0=$EPOCHSECONDS

note() { print -r -- "$*" >&2; }
finish() { (( ${REKI_KEEP:-0} )) || rm -rf "$WORK"; }
trap finish EXIT

# --- helpers ---------------------------------------------------------------

# FFvals prints "p50 p95 p99" in ms for a file of one float per line.
ffvals() { awk '{ v[NR] = $1 } END {
  if (NR < 1) { printf "n/a" } else { printf "%.3f %.3f %.3f",
    v[int(NR*0.50+0.999)], v[int(NR*0.95+0.999)], v[int(NR*0.99+0.999)] } }'
}

# emit_row: one table row. label file
emit_row() {
  local label="$1" f="$2" n vals a b c
  read -r n < "$f" 2>/dev/null
  n=$(wc -l < "$f")
  read -r n <<< "$n"
  if (( n < 1 )); then
    printf '%-8s %6d %6s %7s %7s %7s\n' "$label" 0 ms n/a n/a n/a
    return 0
  fi
  vals="$(sort -n "$f" | ffvals)"
  read -r a b c <<< "$vals"
  printf '%-8s %6d %6s %7s %7s %7s\n' "$label" "$n" ms "$a" "$b" "$c"
  return 0
}

# resolve_bin: build the repo binary if missing. Retry three times so a
# concurrent edit does not sink the run.
resolve_bin() {
  local attempt
  if [[ -x "$REKI_BIN" ]]; then
    REKI_BIN="${REKI_BIN:a}"
    return 0
  fi
  if ! command -v go >/dev/null 2>&1; then
    note "go is unavailable and REKI_BIN does not name an executable"
    return 1
  else
    for attempt in 1 2 3; do
      if ( cd "$REPO_ROOT" && CGO_ENABLED=0 go build -o bin/reki ./cmd/reki ) \
          2>>"$WORK/logs/build.log"; then
        REKI_BIN="${REPO_ROOT}/bin/reki"
        return 0
      fi
      sleep 3
    done
    note "repo go build failed; see $WORK/logs/build.log"
    return 1
  fi
}

# probe_capture: a valid empty command must exit 0 silently. Nonzero means the
# capture subcommand does not work yet; the
# binary is then a stub and the harness still times spawn cost.
probe_capture() {
  if printf '{"command":"","shell":"zsh","exit_code":0}' \
      | "$REKI_BIN" capture >/dev/null 2>>"$WORK/logs/probe.log"; then
    if ! "$REKI_BIN" init >/dev/null 2>>"$WORK/logs/init.log"; then
      note "reki init failed; see $WORK/logs/init.log"
      return 1
    fi
    return 0
  fi
  note "reki capture probe failed; see $WORK/logs/probe.log"
  return 1
}

# write_worker_files: cold-one and 10-shell concurrency worker scripts.
write_worker_files() {
  cat > "$WORK/cold_one.zsh" <<'CKEOF'
emulate -L zsh
setopt nounset
zmodload zsh/datetime
source "$1"
reki_session_init
typeset u=$EPOCHREALTIME d
reki_hook
d=$(( ($EPOCHREALTIME - u) * 1000 ))
print -r -- "$d"
CKEOF
  cat > "$WORK/worker.zsh" <<'WKEOF'
emulate -L zsh
setopt nounset
zmodload zsh/datetime
source "$1"
reki_session_init
typeset -a samples
typeset u d
integer i
for (( i = 1; i <= 30; i++ )); do
  u=$EPOCHREALTIME
  reki_hook
  d=$(( ($EPOCHREALTIME - u) * 1000 ))
  samples+=( "$d" )
  sleep 0.05
done
integer cnt=$#samples
if (( cnt < 1 )); then
  print -r -- "n/a"
else
  print -l -- "${(@)samples}" | sort -n \
    | awk -v n="$cnt" '{ v[NR] = $1 } END {
        printf "%.3f %d\n", v[int(n*0.99+0.999)], n }'
fi
WKEOF
}

# run_variant: warm N cycles + 20-cycle cold probe for one variant.
# NOTE: declare locals one per statement; zsh expands all right-hand sides of
# a single local/typeset command before any of them is assigned.
run_variant() {
  local v="$1"
  [[ -n "$v" ]] || { note "run_variant: empty variant"; return 0; }
  local hf="${BENCH_DIR}/hook_${v}.zsh"
  local tsamples="${WORK}/warm_${v}.txt"
  local csamples="${WORK}/cold_${v}.txt"
  local out k
  : > "$tsamples"
  : > "$csamples"
  (
    emulate -L zsh
    setopt nounset
    unset -f reki_hook reki_session_init 2>/dev/null
    source "$hf"
    typeset -a samples
    typeset u d
    integer i
    for (( i = 1; i <= N; i++ )); do
      u=$EPOCHREALTIME
      reki_hook
      d=$(( ($EPOCHREALTIME - u) * 1000 ))
      samples+=( "$d" )
    done
    print -l -- "${(@)samples}" > "$tsamples"
  ) 2>>"$WORK/logs/warm_${v}.log"
  emit_row "$v" "$tsamples"
  for (( k = 1; k <= 20; k++ )); do
    out="$(zsh -f "$WORK/cold_one.zsh" "$hf" 2>/dev/null)"
    [[ -n "$out" ]] && print -r -- "$out" >> "$csamples"
  done
  emit_row "cold*$v" "$csamples"
  return 0
}

# run_concurrency: 10 parallel zsh -f workers share the same XDG data dir.
run_concurrency() {
  local v="$1"
  [[ -n "$v" ]] || { note "run_concurrency: empty variant"; return 0; }
  local i j f pit attempted
  local -i total_attempted=0
  typeset -a pids p99s
  local before
  before="$(wait_for_stable_rows)"
  for (( i = 1; i <= 10; i++ )); do
    f="$WORK/conc_${v}_${i}.txt"
    zsh -f "$WORK/worker.zsh" "${BENCH_DIR}/hook_${v}.zsh" > "$f" \
      2>>"$WORK/logs/conc_${v}.log" &
    pids+=( $! )
  done
  for j in "${(@)pids}"; do wait "$j" 2>/dev/null; done
  for (( i = 1; i <= 10; i++ )); do
    if [[ -s "$WORK/conc_${v}_${i}.txt" ]]; then
      read -r pit attempted < "$WORK/conc_${v}_${i}.txt"
      p99s+=( "w${i}=${pit}" )
      (( total_attempted += ${attempted:-0} ))
    else
      p99s+=( "w${i}=n/a" )
    fi
  done
  local after inserted="unavailable"
  if [[ "$before" == <-> ]]; then
    after="$(wait_for_row_target $(( before + total_attempted )))"
    [[ "$after" == <-> ]] && inserted=$(( after - before ))
  fi
  print -r -- "$(print -l -- "${(@)p99s}" | tr '\n' ' ')attempted=${total_attempted} inserted=${inserted}"
  return 0
}

rows_count() {
  command -v sqlite3 >/dev/null 2>&1 || { print -r -- "unavailable"; return 0; }
  [[ -f "$DB" ]] || { print -r -- "no-db"; return 0; }
  sqlite3 "$DB" 'SELECT count(*) FROM history;' 2>/dev/null || print -r -- "query-failed"
  return 0
}

wait_for_stable_rows() {
  local current last="" stable=0
  integer deadline=$(( EPOCHSECONDS + 7 ))
  while (( EPOCHSECONDS <= deadline )); do
    current="$(rows_count)"
    [[ "$current" == <-> ]] || { print -r -- "$current"; return 0; }
    if [[ "$current" == "$last" ]]; then
      (( stable++ ))
      (( stable >= 5 )) && { print -r -- "$current"; return 0; }
    else
      stable=0
      last="$current"
    fi
    sleep 0.2
  done
  print -r -- "$current"
}

wait_for_row_target() {
  local target="$1" current
  integer deadline=$(( EPOCHSECONDS + 7 ))
  while (( EPOCHSECONDS <= deadline )); do
    current="$(rows_count)"
    [[ "$current" == <-> ]] || { print -r -- "$current"; return 0; }
    (( current >= target )) && { print -r -- "$current"; return 0; }
    sleep 0.2
  done
  print -r -- "$current"
}

# --- main -------------------------------------------------------------------

resolve_bin || exit 1
export REKI_BIN
probe_capture || exit 1
write_worker_files

typeset -g BINTAG="${REKI_BIN}"
[[ "$REKI_BIN" == "${REPO_ROOT}/bin/reki" ]] && BINTAG="bin/reki (repo build)"
print -- "--- reki hook benchmark -------------------------------------"
print -r -- "host      : $(uname -sm) / zsh $(zsh -f -c 'print $ZSH_VERSION' 2>/dev/null)"
print -r -- "work dir  : $WORK"
print -r -- "binary    : $BINTAG"
print -r -- "n/variant : $N / $VARIANT"
print ""
printf '%-8s %6s %6s %7s %7s %7s\n' variant N '' p50 p95 p99
printf '%-8s %6s %6s %7s %7s %7s\n' ------ ------ ------ ------ ------ ------

typeset -g variants=()
[[ "$VARIANT" == fg || "$VARIANT" == both ]] && variants+=( fg )
[[ "$VARIANT" == bg || "$VARIANT" == both ]] && variants+=( bg )
(( ${#variants} )) || variants=( fg bg )

typeset -g ci
for ci in "${(@)variants}"; do
  run_variant "${ci}"
done

print ""
print -- "--- concurrency: 10 parallel zsh -f workers x 30 captures, per-worker p99 (ms) ---"
for ci in "${(@)variants}"; do
  typeset -g acct
  acct="$(run_concurrency "${ci}")"
    print -r -- "  ${ci}: ${acct}"
done
print -r -- "  rows in DB: $(rows_count)"

typeset -gi T1=$EPOCHSECONDS
print ""
print -r -- "elapsed: $(( $T1 - $T0 )) s   (cold*: fresh zsh -f process, page-cache effects approximate)"
exit 0
