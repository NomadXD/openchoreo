#!/usr/bin/env bash
# Copyright 2026 The OpenChoreo Authors
# SPDX-License-Identifier: Apache-2.0
#
# Blue/green: the EU VAT switch at midnight on 1 July 2021.
# Run from the repository root after `make -C samples/progressive-delivery images`.
source "$(dirname "$0")/../lib.sh"
cd "$(dirname "$0")"

# vat prints what one checkout charges a German buyer, via the gateway.
vat() {
  curl -s "$@" "$(invoke_url checkout)/?country=DE&amount=100"
  echo
}

bold "The story"
say "From 1 July 2021, an online shop selling across the EU must charge VAT at the buyer's"
say "country rate (the One-Stop Shop rules), not its own. A German buyer of a Dutch shop pays"
say "19%, not 21%. At midnight every checkout must switch at once: a canary would charge some"
say "buyers the wrong tax. So run both versions, test green in production, and flip."
pause

bold "1. Blue serves everyone; QA previews green with a header"
run kubectl apply -f components.yaml
run kubectl apply -f 01-preview.yaml
wait_ready checkout
say "A buyer:"
vat
say "QA, with x-preview: green:"
vat -H 'x-preview: green'
both checkout "/?country=DE"
pause

if [[ "${OPTIONAL_PATH:-0}" == "1" ]]; then
  bold "1b. The same preview by path, for testers who can't send a header"
  run kubectl apply -f 01b-preview-by-path.yaml
  wait_ready checkout
  say "A buyer:"
  vat
  say "QA, under /checkout/next:"
  curl -s "$(invoke_url checkout)/next/?country=DE&amount=100"
  echo
  both checkout "/next/?country=DE"
  say "Back to the header preview for the rest of the demo:"
  run kubectl apply -f 01-preview.yaml
  wait_ready checkout
  pause
fi

bold "2. 00:00, 1 July: switch everyone to green"
set_weights checkout checkout-blue=0 checkout-green=100
vat
both checkout "/?country=DE"
say "Releases: $(release_count checkout). The switch was a binding edit, not a new release."
pause

bold "3. A bug in green? Switch back in seconds"
set_weights checkout checkout-blue=100 checkout-green=0
vat
say "The preview header still reaches green, so the fix can be checked before the next switch:"
vat -H 'x-preview: green'
pause

bold "4. Switch again, and retire blue once green has held"
set_weights checkout checkout-blue=0 checkout-green=100
set_weights checkout checkout-green=100
run kubectl apply -f 04-retire-blue.yaml
wait_ready checkout
run kubectl delete component checkout-blue -n "$NS"
both checkout "/?country=DE"
