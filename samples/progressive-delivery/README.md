# Progressive delivery with Endpoints

Four demos of progressive delivery, each built on one Endpoint, plus two
EndpointTypes that render the backends' matches and mirrors.

| Demo | Strategy | Question it answers | Real-world story |
| --- | --- | --- | --- |
| [Canary: rewriting trips in Go](canary-trips-rewrite/) | A testers header, then weights stepped 5 → 25 → 50 → 100%, with a rollback | Is the new code safe? | Uber rewrote its Schemaless sharding layer from Python to Go and moved traffic over by percentage |
| [Blue/green: the EU VAT switch](bluegreen-eu-vat/) | A header (or path) preview, then an all-at-once switch | Can every request change at one moment? | The EU One-Stop Shop VAT rules took effect at 00:00 on 1 July 2021 |
| [A/B: cart recommendations](ab-cart-recommendations/) | Cookie groups, optionally mobile only | Is the new feature better? | Amazon's "customers who bought this also bought" in the cart |
| [Mirror: shadowing the trips rewrite](mirror-trips-shadow/) | Copies of 10%, then 100% of requests, then a canary | Does the new code survive real traffic before anyone depends on it? | Dark launches (Google SRE); Twitter's Diffy |

## What the Endpoint gives you

- **One Endpoint, several components.** The Endpoint lists each version as a
  backend (`spec.backendRefs`) with a weight. A canary adds a backend; it doesn't
  need a new Endpoint.
- **Path, header and cookie routing on a backend.** A backend's `match` sends
  every request that meets all of its conditions to that backend, whatever the
  weights; everything else is split by weight. A backend at weight 0 with a
  match is a preview. A backend's `mirror` copies a share of the split to it
  and discards its answers.
- **Per-environment weights without a release.** A binding's
  `spec.backendOverrides` changes a backend's weight or mirror share in one
  environment, in seconds, and cuts no new release. Matches keep working while
  the split is switched or rolled back.
- **Types read `${rules}`.** Next to `${backends}`, an EndpointType gets
  `${rules}`: one rule per matched backend, in order, then the default rule
  with the weighted split and the mirrors. Header names arrive lowercased and
  the cookie as a regex on the Cookie header, so every data plane matches the
  same requests. The types route to each backend component's own Service.
  - [`endpoint-types/http.yaml`](endpoint-types/http.yaml)
    (`http-progressive`): a Gateway API route per external or internal
    visibility, used by the demos.
  - [`endpoint-types/istio-virtualservice.yaml`](endpoint-types/istio-virtualservice.yaml):
    the same rules for in-cluster callers through an Istio sidecar mesh.
- **No downtime.** A candidate starts at weight 0, so it gets traffic only
  when you raise its share. Overrides for a backend a release removed are
  ignored, so the release promotes as is.
- **`autoDeploy`.** The Endpoint controller binds each new release to the
  first environment of the project's deployment pipeline.

## Running the demos

You need the local k3d setup with this branch's controller and the
`http-progressive` ClusterEndpointType.

```shell
kubectl apply -f samples/progressive-delivery/endpoint-types/http.yaml
make -C samples/progressive-delivery images     # builds the apps, imports them into k3d
make -C samples/progressive-delivery canary      # or bluegreen, ab, mirror
make -C samples/progressive-delivery clean
```

Each `demo.sh` narrates and pauses between steps; `AUTO=1` runs it straight
through. `MESH=1` also tallies calls from a pod in the cluster to the stable
component's own address; it needs Istio with sidecars and the
[`istio-virtualservice`](endpoint-types/istio-virtualservice.yaml) type on the
Endpoint. `REQUESTS` sets how many requests each tally sends (default 100).
`OPTIONAL_PATH=1` adds the blue/green preview by path.

The apps are deliberately tiny. [`apps/trips-python`](apps/trips-python) is the
original trips service. [`apps/demo-go`](apps/demo-go) serves the Go rewrite,
the checkout and the storefront, chosen by `MODE`. Every response names its
backend in the `X-Served-By` header, which the tallies count, and `demo-go`
counts the requests and mirrored copies it receives on `/stats`.
