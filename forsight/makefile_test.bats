#!/usr/bin/env bats
# Tests for `make help` (quality-dx-10): every public target gets a one-line
# description generated from the `##` comment placed above it, `help` is the
# default goal, and the list can't drift from the targets themselves because
# it's derived from the same Makefile bats and `make` both read. Run with
# `bats makefile_test.bats` (brew install bats-core or apt-get install bats);
# not part of `go test ./...`, same reasoning as install_test.bats.

setup() {
  MAKEFILE="$(cd "$(dirname "$BATS_TEST_FILENAME")" && pwd)/Makefile"
  cd "$(dirname "$MAKEFILE")"
}

# Every target this Makefile declares .PHONY, in the order it lists them —
# the list `make help` must reproduce line-for-line.
PHONY_TARGETS="help build build-web build-go release dev demo test test-install lint vet clean"

@test "make with no arguments runs help (help is .DEFAULT_GOAL)" {
  run make
  [ "$status" -eq 0 ]
  bare_run_output="$output"

  run make help
  [ "$status" -eq 0 ]

  [ "$bare_run_output" = "$output" ]
}

@test "make help lists every .PHONY target with a non-empty description" {
  run make help
  [ "$status" -eq 0 ]

  for target in $PHONY_TARGETS; do
    line=$(printf '%s\n' "$output" | grep -E "^  ${target}[[:space:]]")
    [ -n "$line" ]
    desc=$(printf '%s\n' "$line" | sed -E "s/^  ${target}[[:space:]]+//")
    [ -n "$desc" ]
  done
}

@test "make help emits exactly one line per ## comment in the Makefile" {
  run make help
  [ "$status" -eq 0 ]

  comment_count=$(grep -c '^## ' "$MAKEFILE")
  output_lines=$(printf '%s\n' "$output" | grep -c '^  ')

  [ "$comment_count" -eq "$output_lines" ]
}

@test "every ## comment sits directly above the target it describes" {
  # Guards against the comment/target pairing drifting apart (a blank line,
  # a reordered target) in a way `grep -c` alone wouldn't catch.
  run awk '/^## / { line = NR } /^[a-zA-Z_][a-zA-Z0-9_.-]*:/ { if (line == NR - 1) count++ } END { print count }' "$MAKEFILE"
  [ "$status" -eq 0 ]

  comment_count=$(grep -c '^## ' "$MAKEFILE")
  [ "$output" -eq "$comment_count" ]
}
