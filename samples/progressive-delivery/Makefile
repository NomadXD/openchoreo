# Builds the demo apps and loads them into the local k3d cluster.
#
#   make -C samples/progressive-delivery images
#   make -C samples/progressive-delivery canary | bluegreen | ab | mirror
#   make -C samples/progressive-delivery clean

K3D_CLUSTER_NAME ?= openchoreo
IMAGES := openchoreo-demo/trips-python:local openchoreo-demo/demo-go:local

.PHONY: images canary bluegreen ab mirror clean

images:
	docker build -t openchoreo-demo/trips-python:local apps/trips-python
	docker build -t openchoreo-demo/demo-go:local apps/demo-go
	k3d image import -c $(K3D_CLUSTER_NAME) $(IMAGES)

canary:
	./canary-trips-rewrite/demo.sh

bluegreen:
	./bluegreen-eu-vat/demo.sh

ab:
	./ab-cart-recommendations/demo.sh

mirror:
	./mirror-trips-shadow/demo.sh

# Deletes the demos' Endpoints and Components. The Endpoints go first, so no
# component is still receiving traffic when it is deleted.
clean:
	-kubectl delete endpoints.openchoreo.dev -n default trips-api checkout cart --ignore-not-found
	-kubectl delete component -n default trips-python trips-go checkout-blue checkout-green storefront storefront-recs --ignore-not-found
