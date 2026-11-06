# A/B: recommendations in the cart

**Strategy:** cookie groups, with an optional mobile-only step.
**Question:** is the new feature better?

## The story

Greg Linden, an engineer at Amazon, wanted the cart to suggest items other
customers had bought. A senior vice president feared it would distract shoppers
from paying, and forbade it. Linden tested it anyway, and it won by enough that
it shipped ([Greg Linden](https://glinden.blogspot.com/2006/04/early-amazon-shopping-cart.html)).

An A/B test needs each visitor to stay in one group for the whole experiment,
or their behaviour can't be attributed. So the storefront assigns a group once,
50/50, in the `exp_cartrecs` cookie, and the Endpoint routes on the cookie.
Gateway API session persistence can't hold a visitor in an arm by itself
([GEP-1619](https://gateway-api.sigs.k8s.io/geps/gep-1619/)), so assignment
happens upstream, as in
[Cloudflare's A/B testing pattern](https://developers.cloudflare.com/workers/examples/ab-testing/).

## Steps

| Step | What changes | What the audience sees |
| --- | --- | --- |
| 1. [`components.yaml`](components.yaml), [`01-experiment.yaml`](01-experiment.yaml) | Match on storefront-recs: cookie `exp_cartrecs=b` | Group b always gets recommendations, and group a never does |
| 2. [`02-mobile-only.yaml`](02-mobile-only.yaml) (optional) | Match on storefront-recs: the cookie AND a mobile User-Agent | Phones in group b get recommendations; desktops in group b don't |
| 3. [`03-ship-winner.yaml`](03-ship-winner.yaml) | storefront-recs only | Everyone gets recommendations |

The first page view of a new visitor is served before the group is assigned;
every later view follows the cookie.

## What to point out

- An A/B test routes on who the visitor is, not on a percentage of requests.
- The conditions of a match must all hold, so "group b AND on a phone" is one
  match.

## References

- [Greg Linden: Early Amazon: shopping cart recommendations](https://glinden.blogspot.com/2006/04/early-amazon-shopping-cart.html)
- [Kohavi and Thomke: The surprising power of online experiments (HBR)](https://exp-platform.com/hbr-the-surprising-power-of-online-experiments/)
- [Gateway API GEP-1619: session persistence](https://gateway-api.sigs.k8s.io/geps/gep-1619/)
- [GrowthBook: sticky bucketing](https://docs.growthbook.io/app/sticky-bucketing)
- [Flagger: Istio A/B testing](https://docs.flagger.app/tutorials/istio-ab-testing)
