#!/usr/bin/env bash
# Copyright 2026 The OpenChoreo Authors
# SPDX-License-Identifier: Apache-2.0
#
# A/B: should the cart show "Customers who bought this also bought"?
# Run from the repository root after `make -C samples/progressive-delivery images`.
source "$(dirname "$0")/../lib.sh"
cd "$(dirname "$0")"

MOBILE='Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) Mobile/15E148'
DESKTOP='Mozilla/5.0 (Macintosh; Intel Mac OS X 14_0) Safari/605.1.15'

# visitors simulates N new visitors: each one's first page view assigns a
# group, and their next views carry the cookie. It counts who served them.
visitors() {
  local url
  url="$(invoke_url cart)"
  for i in $(seq 1 "${VISITORS:-40}"); do
    jar="$(mktemp)"
    curl -s -o /dev/null -c "$jar" "$@" "$url/"
    for _ in 1 2 3; do
      curl -s -o /dev/null -D - -b "$jar" "$@" "$url/" | tr -d '\r' |
        awk -v g="$(awk '/exp_cartrecs/{print $7}' "$jar")" 'tolower($1)=="x-served-by:"{print "group " g " -> " $2}'
    done
    rm -f "$jar"
  done | sort | uniq -c | awk '{printf "    %4d  %s %s %s %s\n", $1, $2, $3, $4, $5}'
}

bold "The story"
say "An engineer at Amazon wanted the cart to suggest items other customers bought. A senior"
say "VP worried it would distract people from paying. Opinions don't settle that; a test"
say "does. Each visitor is put in group A or B once, and stays there, so you can compare"
say "orders and basket size between the groups."
pause

bold "1. The experiment: the cookie decides the group"
run kubectl apply -f components.yaml
run kubectl apply -f 01-experiment.yaml
wait_ready cart
visitors
say "Group b always gets recommendations (storefront-recs), group a never does."
say "A request that carries the group b cookie:"
tally "$(invoke_url cart)/" -H 'Cookie: exp_cartrecs=b'
pause

bold "2. Optional: mobile shoppers only"
run kubectl apply -f 02-mobile-only.yaml
wait_ready cart
say "Phones:"
visitors -A "$MOBILE"
say "Desktops (group b too gets the plain cart):"
visitors -A "$DESKTOP"
pause

bold "3. B won: ship it to everyone"
run kubectl apply -f 03-ship-winner.yaml
wait_ready cart
run kubectl delete component storefront -n "$NS"
visitors
