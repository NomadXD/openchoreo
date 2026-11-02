#!/usr/bin/env bash
# Copyright 2026 The OpenChoreo Authors
# SPDX-License-Identifier: Apache-2.0
#
# Shared helpers for the progressive delivery demos. Source it from a demo.sh.

set -euo pipefail

NS="${NS:-default}"
ENV_NAME="${ENV_NAME:-development}"
# AUTO=1 runs without pausing between steps.
AUTO="${AUTO:-0}"
# REQUESTS is how many requests each tally sends per path.
REQUESTS="${REQUESTS:-100}"

bold() { printf '\n\033[1m%s\033[0m\n' "$*"; }
say() { printf '  %s\n' "$*"; }

pause() {
  if [[ "$AUTO" != "1" ]]; then
    read -r -p "  [enter] " _
  fi
}

# run prints a command, then runs it.
run() {
  printf '  \033[2m$ %s\033[0m\n' "$*"
  "$@"
}

# binding prints the name of the Endpoint's binding in ENV_NAME.
binding() { echo "$1-$ENV_NAME"; }

# wait_ready waits until the Endpoint's binding is Ready on its latest release.
wait_ready() {
  local ep="$1" b latest
  b="$(binding "$ep")"
  for _ in $(seq 1 90); do
    latest="$(kubectl get endpoints.openchoreo.dev "$ep" -n "$NS" -o jsonpath='{.status.latestRelease.name}' 2>/dev/null || true)"
    if [[ -n "$latest" ]] &&
      [[ "$(kubectl get endpointreleasebinding "$b" -n "$NS" -o jsonpath='{.spec.endpointRelease}' 2>/dev/null)" == "$latest" ]] &&
      [[ "$(kubectl get endpointreleasebinding "$b" -n "$NS" -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null)" == "True" ]]; then
      wait_backends "$ep"
      wait_served "$ep"
      return 0
    fi
    sleep 2
  done
  echo "binding $b did not become Ready; kubectl describe endpointreleasebinding $b -n $NS" >&2
  return 1
}

# wait_backends waits until the Deployments of the binding's backend
# components are available, so the first requests don't hit pods that are
# still starting.
wait_backends() {
  local ep="$1" dpns c
  dpns="$(dp_namespace "$ep")"
  for c in $(kubectl get endpoints.openchoreo.dev "$ep" -n "$NS" -o jsonpath='{.spec.backendRefs[*].component.name}'); do
    kubectl wait deployment -n "$dpns" -l "openchoreo.dev/component=$c" --for=condition=Available --timeout=180s >/dev/null 2>&1 || true
  done
}

# wait_served waits until a backend answers through the gateway. The binding
# turns Ready once the data plane has the routes, a few seconds before the
# gateway has programmed them and sees the new Services; until then it
# answers 500.
wait_served() {
  local url
  url="$(invoke_url "$1")"
  for _ in $(seq 1 60); do
    if curl -s -o /dev/null -D - "$url" | tr -d '\r' | grep -qi '^x-served-by:'; then
      return 0
    fi
    sleep 1
  done
  echo "the gateway does not serve $url yet" >&2
}

# wait_weights waits until the binding reports the given effective weights
# and the data plane has accepted the new routes, then lets Envoy settle.
wait_weights() {
  local ep="$1" want got names
  shift
  want="$(printf '%s\n' "$@" | sort | tr '\n' ' ')"
  names="$(printf '%s ' "${@%%=*}")"
  for _ in $(seq 1 60); do
    # status.backends omits a weight of 0.
    got="$(kubectl get endpointreleasebinding "$(binding "$ep")" -n "$NS" \
      -o jsonpath='{range .status.backends[*]}{.name}={.weight}{"\n"}{end}' |
      awk -F= -v names=" $names" 'index(names, " " $1 " ") {print $1 "=" ($2 == "" ? 0 : $2)}' | sort | tr '\n' ' ')"
    if [[ "$got" == "$want" ]] &&
      [[ "$(kubectl get endpointreleasebinding "$(binding "$ep")" -n "$NS" -o jsonpath='{.status.conditions[?(@.type=="ResourcesReady")].status}')" == "True" ]]; then
      sleep "${SETTLE_SECONDS:-5}"
      return 0
    fi
    sleep 1
  done
  echo "the binding of $ep did not reach weights: $want" >&2
}

# set_weights patches the binding's backendOverrides, e.g.
# set_weights trips-api trips-python=95 trips-go=5. No new release is cut.
set_weights() {
  local ep="$1" overrides="" sep=""
  shift
  for kv in "$@"; do
    overrides+="$sep{\"name\":\"${kv%%=*}\",\"weight\":${kv#*=}}"
    sep=","
  done
  run kubectl patch endpointreleasebinding "$(binding "$ep")" -n "$NS" --type merge \
    -p "{\"spec\":{\"backendOverrides\":[$overrides]}}"
  wait_weights "$ep" "$@"
}

# release_count prints how many EndpointReleases the Endpoint has.
release_count() {
  kubectl get endpointreleases -n "$NS" \
    -o jsonpath="{range .items[?(@.spec.owner.endpointName==\"$1\")]}{.metadata.name}{\"\\n\"}{end}" | grep -c . || true
}

# invoke_url prints the Endpoint's external URL in ENV_NAME, from the URL the
# EndpointType resolved into status.urls.external, without a trailing slash.
invoke_url() {
  local scheme host port path
  read -r scheme host port path < <(kubectl get endpointreleasebinding "$(binding "$1")" -n "$NS" \
    -o jsonpath='{.status.urls.external.scheme} {.status.urls.external.host} {.status.urls.external.port}{" "}{.status.urls.external.path}')
  # An empty port reads as the path; the path always starts with /.
  if [[ "$port" == /* ]]; then path="$port" port=""; fi
  path="${path%/}"
  echo "${scheme:-http}://$host${port:+:$port}$path"
}

# dp_namespace prints the data-plane namespace of the Endpoint's backends,
# from the in-cluster address the binding resolved for its first backend.
dp_namespace() {
  kubectl get endpointreleasebinding "$(binding "$1")" -n "$NS" \
    -o jsonpath='{.status.backends[0].serviceURL.host}' | cut -d. -f2
}

# tally sends REQUESTS requests to URL with optional curl arguments and counts
# who answered (X-Served-By) and the status codes.
tally() {
  local url="$1"
  shift
  for _ in $(seq 1 "$REQUESTS"); do
    curl -s -o /dev/null -D - "$@" "$url" | tr -d '\r' |
      awk 'NR==1{code=$2} tolower($1)=="x-served-by:"{who=$2} END{print (who==""?"-":who), code}'
  done | sort | uniq -c | awk '{printf "    %4d  %-18s HTTP %s\n", $1, $2, $3}'
}

# both tallies the gateway path. SUFFIX is appended to the URL (for example
# "/?country=DE"); curl arguments follow it.
both() {
  local ep="$1" suffix="${2:-}" url
  shift 2 || shift $#
  url="$(invoke_url "$ep")$suffix"
  say "through the gateway ($url):"
  tally "$url" "$@"
}

# set_mirror patches the binding's backendOverrides to copy a share of the
# requests to a mirrored backend, e.g. set_mirror trips-api trips-go=100. It
# waits until the binding reports that share. No new release is cut.
set_mirror() {
  local ep="$1" name="${2%%=*}" percent="${2#*=}" got
  run kubectl patch endpointreleasebinding "$(binding "$ep")" -n "$NS" --type merge \
    -p "{\"spec\":{\"backendOverrides\":[{\"name\":\"$name\",\"mirror\":{\"percent\":$percent}}]}}"
  for _ in $(seq 1 60); do
    got="$(kubectl get endpointreleasebinding "$(binding "$ep")" -n "$NS" \
      -o jsonpath="{.status.backends[?(@.name==\"$name\")].mirror.percent}")"
    if [[ "${got:-0}" == "$percent" ]] &&
      [[ "$(kubectl get endpointreleasebinding "$(binding "$ep")" -n "$NS" -o jsonpath='{.status.conditions[?(@.type=="ResourcesReady")].status}')" == "True" ]]; then
      sleep "${SETTLE_SECONDS:-5}"
      return 0
    fi
    sleep 1
  done
  echo "the binding of $ep did not reach a $percent% mirror to $name" >&2
}

# clear_overrides removes the binding's backendOverrides, so the release's
# weights and mirror shares apply again.
clear_overrides() {
  run kubectl patch endpointreleasebinding "$(binding "$1")" -n "$NS" --type merge \
    -p '{"spec":{"backendOverrides":[]}}'
}

# show_mirror prints the mirror share the binding applies to a backend. A
# copy's answer never reaches the caller, so the caller can't count copies;
# the candidate's own logs and metrics show what it received.
show_mirror() {
  local ep="$1" backend="$2"
  say "mirror share to $backend in $ENV_NAME: $(kubectl get endpointreleasebinding "$(binding "$ep")" -n "$NS" \
    -o jsonpath="{.status.backends[?(@.name==\"$backend\")].mirror.percent}")%"
}
