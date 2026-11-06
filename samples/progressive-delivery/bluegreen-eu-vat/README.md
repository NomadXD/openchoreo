# Blue/green: the EU VAT switch

**Strategy:** a header preview, then an all-at-once switch, with an instant
rollback.
**Question:** can every request change at one moment?

## The story

From 1 July 2021, an online shop selling to consumers across the EU must charge
VAT at the buyer's country rate, under the One-Stop Shop rules
([European Commission](https://vat-one-stop-shop.ec.europa.eu/one-stop-shop_en)).
For a Dutch shop, a German buyer now pays 19%, not the shop's own 21%.

A canary is wrong here: at 00:05 it would charge some buyers the old tax and
some the new. Every checkout must change at once. So the shop runs both
versions. Blue charges the old way and green the new. QA tests green in
production with a header that only they send, and at midnight everyone flips.
If green has a bug, everyone flips back in seconds, because blue is still running.
This is Martin Fowler's
[BlueGreenDeployment](https://martinfowler.com/bliki/BlueGreenDeployment.html).

## Steps

| Step | What changes | What the audience sees |
| --- | --- | --- |
| 1. [`components.yaml`](components.yaml), [`01-preview.yaml`](01-preview.yaml) | Blue at 100, green at 0 with a match: `x-preview: green` → green | Buyers pay 21% (NL). With the header, 19% (DE) |
| 1b. (optional) [`01b-preview-by-path.yaml`](01b-preview-by-path.yaml) | Green's match is a path instead: `/checkout/next` → green | QA previews green in a browser, no header needed |
| 2. [`02-switch.yaml`](02-switch.yaml) | The binding's overrides: blue 0, green 100 | Everyone pays 19%, at once |
| 3. [`03-rollback.yaml`](03-rollback.yaml) | Blue 100, green 0 | Back to 21% in seconds; the preview still reaches green |
| 4. [`04-retire-blue.yaml`](04-retire-blue.yaml) | Green only, blue deleted | Done |

## What to point out

- The preview is a match on green, so it works in production without a
  separate Endpoint. It can be a header, a cookie or a path
  (`OPTIONAL_PATH=1 make bluegreen` shows the path).
- The switch and the rollback edit the binding: no release, no redeploy.
- A match ignores weights, so the preview keeps working whichever way the
  switch points.

## References

- [European Commission: VAT One-Stop Shop](https://vat-one-stop-shop.ec.europa.eu/one-stop-shop_en)
- [Martin Fowler: BlueGreenDeployment](https://martinfowler.com/bliki/BlueGreenDeployment.html)
- [AWS whitepaper: Blue/green deployments](https://docs.aws.amazon.com/whitepapers/latest/blue-green-deployments/introduction.html)
- [Istio: request routing by header](https://istio.io/latest/docs/tasks/traffic-management/request-routing/)
