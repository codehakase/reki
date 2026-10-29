#!/usr/bin/env zsh
# Foreground variant of the zsh hook prototype. Benchmark counterfactual only.
# Hard constraints: $? saved first, restored last; nounset-safe; zero stdout.

: "${REKI_BIN:?run through bench/run.zsh or set REKI_BIN to a reki executable}"

reki_session_init() {
  printf -v REKI_SESSION '%x-%x' "${1:-$$}" \
    $(( (${RANDOM:-0} % 32768) * 32768 + (${RANDOM:-0} % 32768) ))
}
reki_session_init

reki_hook() {
  local ec=$?
  {
    local payload
    # Payload over stdin; fixed benign command text, never a real user command.
    printf -v payload \
      '{"command":"bench-capture","shell":"zsh","exit_code":%d,"session_id":"%s","cwd":"bench"}' \
      "$ec" "${REKI_SESSION:-}"
    # fg: synchronous call; prompt waits for capture exit. stderr stays
    # attached: it is the one-per-session warning path. stdout to /dev/null.
    printf '%s' "$payload" | "$REKI_BIN" capture >/dev/null || true
  } || true
  return $ec  # restore $? as the last action before the prompt continues
}
# nounset-safe registration guard (empty precmd_functions unwinds with nounset)
if (( ${+precmd_functions} )); then
  (( ${precmd_functions[(Ie)reki_hook]} )) || precmd_functions+=(reki_hook)
else
  precmd_functions=(reki_hook)
fi
