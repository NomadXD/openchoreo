#!/usr/bin/env bash
# Copyright 2026 The OpenChoreo Authors
# SPDX-License-Identifier: Apache-2.0
#
# Mirror: shadowing real traffic to the Go rewrite before any rider sees it.
# Run from the repository root after `make -C samples/progressive-delivery images`.
source "$(dirname "$0")/../lib.sh"
cd "$(dirname "$0")"

bold "The story"
say "The trips service is being rewritten in Go. Before a canary gives the rewrite any"
say "rider, copy real requests to it: it serves the traffic, its answers are thrown away,"
say "and the team watches its errors and latency. Riders can't be hurt by what it does."
pause

bold "1. Riders get Python; the gateway copies 10% of requests to Go"
run kubectl apply -f components.yaml
run kubectl apply -f 01-shadow.yaml
wait_ready trips-api
both trips-api ""
say "Every answer comes from trips-python. The copies don't show here; trips-go counts the copies it receives:"
mirror_count trips-api trips-go
pause

bold "2. Copy every request in development"
set_mirror trips-api trips-go=100
mirror_count trips-api trips-go
say "Releases: $(release_count trips-api). The share was a binding edit, not a new release."
pause

bold "3. The rewrite starts failing 30% of requests"
run kubectl patch workload trips-go-workload -n "$NS" --type json \
  -p '[{"op":"replace","path":"/spec/container/env/2/value","value":"0.3"}]'
say "Waiting for the faulty build to roll out..."
sleep "${ROLLOUT_SECONDS:-40}"
both trips-api ""
say "Riders see no errors: the copies fail, but their answers never reach anyone."
say "The rewrite's own logs and metrics show the failures. Fix it before going on."
run kubectl patch workload trips-go-workload -n "$NS" --type json \
  -p '[{"op":"replace","path":"/spec/container/env/2/value","value":"0"}]'
sleep "${ROLLOUT_SECONDS:-40}"
pause

bold "4. It held: turn the shadow into a canary"
clear_overrides trips-api
run kubectl apply -f 03-to-canary.yaml
wait_ready trips-api
both trips-api ""
say "Testers, with x-canary: true:"
both trips-api "" -H 'x-canary: true'
say "Riders are still on Python. Carry on with make canary's steps to move them over."
