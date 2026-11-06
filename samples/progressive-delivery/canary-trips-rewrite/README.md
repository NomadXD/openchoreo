# Canary: rewriting the trips service in Go

**Strategy:** weighted canary, stepped 5 → 25 → 50 → 100%, with a rollback.
**Question:** is the new code safe?

## The story

Riders open "Your trips" all day. The trips service was written in Python and is
slow, so the team rewrote it in Go: same API, new engine. Tests pass, but only
real traffic proves a rewrite. So the Go version gets 5% of requests first.
If error rates and latency hold, it gets more; if not, it goes back to 0 in
seconds.

Uber did this with the sharding layer of its Schemaless datastore: rewritten
from Python to Go, it moved traffic over by percentage and cut median latency by
85% ([Uber](https://www.uber.com/blog/schemaless-rewrite/)). Google's SRE
workbook calls this canarying: a small, representative share first, compared
against the current version ([Canarying releases](https://sre.google/workbook/canarying-releases/)).

## Steps

| Step | What changes | What the audience sees |
| --- | --- | --- |
| 1. [`components.yaml`](components.yaml), [`01-endpoint.yaml`](01-endpoint.yaml) | `trips-python` at 100, `trips-go` staged at 0 with a match: `x-canary: true` → trips-go | Riders answered by trips-python, testers by trips-go |
| 2. Canary: 5, 25, 50% ([`02-canary-5.yaml`](02-canary-5.yaml)) | The binding's `backendOverrides` | The share moves and the release count stays the same |
| 3. Fault | `trips-go` fails 30% of requests | Errors only on the canary's share; one edit puts it back to 0, and testers still reach the rewrite |
| 4. Fix forward, 100% | `trips-go` fixed, then at 100 | All traffic on Go |
| 5. [`03-retire-python.yaml`](03-retire-python.yaml) | Python is dropped from the Endpoint, then the component is deleted | No downtime at any point |

## What to point out

- A canary is a weight on a backend of an existing Endpoint, not a new route.
- Testers reach the canary before any rider does, through its `match`. A
  match ignores weights, so it keeps working at 0%, during the ramp and after
  a rollback.
- Changing weights edits the binding for one environment. Production keeps its
  own split, and no release is cut.

## Retiring the old component

This demo replaces one component with another, so the old one has to be retired in order:

1. Give the new backend all the traffic in every environment.
2. Remove the old backend from the Endpoint. That cuts a release; promote it. Outside callers keep the Endpoint's URL, and overrides that still name the old backend are ignored.
3. Move in-cluster callers that call the old component's own address to the new one. With a mesh they are already served by the new one through the old address.
4. Delete the old component once it receives no requests, and only after the Endpoint no longer lists it.

A new build of the same service doesn't need any of this: run it as a release line of the same component, and its address never changes.

## References

- [Uber: Code migration in production: rewriting the sharding layer of Schemaless](https://www.uber.com/blog/schemaless-rewrite/)
- [Google SRE workbook: Canarying releases](https://sre.google/workbook/canarying-releases/)
- [Netflix and Google: Kayenta automated canary analysis](https://cloud.google.com/blog/products/gcp/introducing-kayenta-an-open-automated-canary-analysis-tool-from-google-and-netflix)
- [Gateway API: traffic splitting](https://gateway-api.sigs.k8s.io/guides/traffic-splitting/)
