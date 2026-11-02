#!/usr/bin/env bash
# Copyright 2026 The OpenChoreo Authors
# SPDX-License-Identifier: Apache-2.0
#
# Canary: moving the trips service from Python to Go without downtime.
# Run from the repository root after `make -C samples/progressive-delivery images`.
source "$(dirname "$0")/../lib.sh"
cd "$(dirname "$0")"

bold "The story"
say "Riders open 'Your trips' millions of times a day. The Python trips service is slow,"
say "so the team rewrote it in Go: same API, new engine. Is the new code safe?"
say "Send it a small share of real traffic first, watch, then widen."
pause

bold "1. Deploy both versions, and one Endpoint that sends everything to Python"
run kubectl apply -f components.yaml
run kubectl apply -f 01-endpoint.yaml
wait_ready trips-api
both trips-api ""
say "trips-go is deployed and resolved at weight 0: no rider sees it yet."
say "Testers, with x-canary: true, already reach it:"
both trips-api "" -H 'x-canary: true'
pause

for w in 5 25 50; do
  bold "2. Canary at ${w}%"
  set_weights trips-api trips-python=$((100 - w)) trips-go=$w
  both trips-api ""
  say "Same split from the gateway and from inside the cluster: one Endpoint, one split."
  say "Releases cut so far: $(release_count trips-api) (a weight change is not a release)"
  pause
done

bold "3. Something goes wrong: the rewrite starts timing out on 30% of requests"
run kubectl patch workload trips-go-workload -n "$NS" --type json \
  -p '[{"op":"replace","path":"/spec/container/env/2/value","value":"0.3"}]'
say "Waiting for the faulty build to roll out..."
sleep "${ROLLOUT_SECONDS:-40}"
both trips-api ""
say "Errors, but only on the canary's share. Roll back: one edit, seconds."
pause
set_weights trips-api trips-python=100 trips-go=0
both trips-api ""
say "Testers still reach the rewrite, so the fix can be checked before riders see it again:"
both trips-api "" -H 'x-canary: true'
pause

bold "4. Fix forward, then finish the migration"
run kubectl patch workload trips-go-workload -n "$NS" --type json \
  -p '[{"op":"replace","path":"/spec/container/env/2/value","value":"0"}]'
sleep "${ROLLOUT_SECONDS:-40}"
set_weights trips-api trips-python=0 trips-go=100
both trips-api ""
pause

bold "5. Retire Python"
set_weights trips-api trips-go=100
run kubectl apply -f 03-retire-python.yaml
wait_ready trips-api
run kubectl delete component trips-python -n "$NS"
both trips-api ""
say "Done: the rewrite serves every rider, and no request saw downtime."
