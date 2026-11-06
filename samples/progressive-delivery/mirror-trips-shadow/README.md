# Mirror: shadowing the trips rewrite

**Strategy:** copy a share of real requests to the new version, which answers
nobody, then turn it into a canary.
**Question:** does the new code survive real traffic, before anyone depends on it?

## The story

The trips service is being rewritten from Python to Go (the
[canary demo](../canary-trips-rewrite/) tells that story). A canary hands the
rewrite real riders, even if only 5% of them. A mirror goes one step earlier:
the gateway copies requests to the rewrite and throws its answers away, so the
rewrite serves production traffic while every rider still gets Python. The
team watches its errors and latency, and only then starts a canary.

Google's SRE book calls this dark launching
([Reliable product launches at scale](https://sre.google/sre-book/reliable-product-launches/)),
and Twitter built [Diffy](https://github.com/opendiffy/diffy) to compare the
mirrored answers with the live ones.

## Steps

| Step | What changes | What the audience sees |
| --- | --- | --- |
| 1. [`components.yaml`](components.yaml), [`01-shadow.yaml`](01-shadow.yaml) | `trips-python` at 100, `trips-go` at 0 with `mirror: { percent: 10 }` | Every answer from trips-python; the gateway counts about 10 copies per 100 requests to trips-go |
| 2. [`02-shadow-all.yaml`](02-shadow-all.yaml) | The binding's override: `trips-go` mirror 100% | 100 copies per 100 requests, in development only, with no new release |
| 3. Fault | `trips-go` fails 30% of requests | Riders see no errors; the copies fail where only the team looks |
| 4. [`03-to-canary.yaml`](03-to-canary.yaml) | The mirror goes; `x-canary: true` → trips-go, still at 0 | Riders on Python, testers on Go; carry on with the canary demo |

## What to point out

- A mirror is a field on a backend, like a match. A backend at weight 0 either
  previews (a `match`) or shadows (a `mirror`), never both.
- The copies go out with the split for every request, so they see the same mix
  of traffic riders send. Only the backend's answers are dropped.
- The share per environment is a binding override, like a weight: development
  can copy everything while production copies 10%, from the same release.
- A copy changes nothing for the caller, but it does run the rewrite's code.
  Mirror only what is safe to run twice: reads, or writes to a separate store.
- Linkerd has no request mirroring, so the types refuse mirrors on a Linkerd
  data plane.

## References

- [Google SRE book: Reliable product launches at scale (dark launches)](https://sre.google/sre-book/reliable-product-launches/)
- [Twitter / Opendiffy: Diffy](https://github.com/opendiffy/diffy)
- [Gateway API: HTTP request mirroring](https://gateway-api.sigs.k8s.io/guides/http-request-mirroring/)
- [Istio: Mirroring](https://istio.io/latest/docs/tasks/traffic-management/mirroring/)
