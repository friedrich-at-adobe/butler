SERVICE_NAME=butler
BUILDER_TAG?=$(or $(sha),$(SERVICE_NAME)-builder)
TESTER_TAG?=$(or $(sha),$(SERVICE_NAME)-tester)

IMAGE_TAG=$(SERVICE_NAME)-img

GO:=go
pkgs=$(shell $(GO) list ./... | egrep -v "(vendor)")

ARTIFACTORY_USER=$(shell echo "$$ARTIFACTORY_USER")
ARTIFACTORY_REPO=butler
ARTIFACTORY_VERSION=1.0.0
ARTIFACTORY_PROD_HOST=docker-ethos-core-univ-release.dr-uw2.adobeitc.com
ARTIFACTORY_DEV_HOST=docker-ethos-core-univ-dev.dr-uw2.adobeitc.com


default: ci

ci: build
	@echo "Success"

build:
	@$(GO) fmt $(pkgs)
	@docker build -t $(BUILDER_TAG) -f Dockerfile-build .
	@docker run -v m2:/root/.m2 -v `pwd`:/build $(BUILDER_TAG) cp /root/butler/butler /build
	@docker build -t $(IMAGE_TAG) .

build-local:
	@$(GO) fmt $(pkgs)
	@$(GO) build butler.go promfuncs.go

pre-deploy-build:
	@docker build -t $(TESTER_TAG) -f Dockerfile-test .
	@docker run -it --rm $(TESTER_TAG)

post-deploy-build:
	@echo "Nothing is defined in post-deploy-build step"

test:
	@docker build -t $(TESTER_TAG) -f Dockerfile-test .
	@docker run -it --rm $(TESTER_TAG)

build-$(ARTIFACTORY_REPO):
	@docker build -t $(ARTIFACTORY_REPO):$(ARTIFACTORY_VERSION) .

push-$(ARTIFACTORY_REPO)-release: DOCKER_IMAGE_ID = $(shell docker images -q $(ARTIFACTORY_REPO):$(ARTIFACTORY_VERSION))
push-$(ARTIFACTORY_REPO)-release: build-$(ARTIFACTORY_REPO)
	@printf "Enter DockerHub "
	@docker login -u $(ARTIFACTORY_USER) $(ARTIFACTORY_PROD_HOST)
	docker tag $(DOCKER_IMAGE_ID) $(ARTIFACTORY_PROD_HOST)/ethos/$(ARTIFACTORY_REPO):$(ARTIFACTORY_VERSION)
	docker push $(ARTIFACTORY_PROD_HOST)/ethos/$(ARTIFACTORY_REPO):$(ARTIFACTORY_VERSION)

push-$(ARTIFACTORY_REPO)-dev: DOCKER_IMAGE_ID = $(shell docker images -q $(ARTIFACTORY_REPO):$(ARTIFACTORY_VERSION))
push-$(ARTIFACTORY_REPO)-dev: build-$(ARTIFACTORY_REPO)
	@printf "Enter DockerHub "
	@docker login -u $(ARTIFACTORY_USER) $(ARTIFACTORY_DEV_HOST)
	docker tag $(DOCKER_IMAGE_ID) $(ARTIFACTORY_DEV_HOST)/ethos/$(ARTIFACTORY_REPO):$(ARTIFACTORY_VERSION)
	docker push $(ARTIFACTORY_DEV_HOST)/ethos/$(ARTIFACTORY_REPO):$(ARTIFACTORY_VERSION)

push-butler-dockerhub: DOCKER_IMAGE_ID = $(shell docker images -q $(ARTIFACTORY_REPO):$(ARTIFACTORY_VERSION))
push-butler-dockerhub: build-$(ARTIFACTORY_REPO)
	@printf "Enter DockerHub "
	@docker login -u $(ARTIFACTORY_USER)
	docker tag $(DOCKER_IMAGE_ID) $(ARTIFACTORY_USER)/$(ARTIFACTORY_REPO):$(ARTIFACTORY_VERSION)
	docker push $(ARTIFACTORY_USER)/$(ARTIFACTORY_REPO):$(ARTIFACTORY_VERSION)

help:
	@printf "Usage:\n\n"
	@printf "make\t\t\t\tBuilds butler, for use in CI.\n"
	@printf "make build-local\t\tBuilds a local binary of butler.\n"
	@printf "make build-$(ARTIFACTORY_REPO)\t\tBuilds butler locally, for use in pushing to artifactory.\n"
	@printf "make push-$(ARTIFACTORY_REPO)-dev\t\tPushes butler to $(ARTIFACTORY_DEV_HOST).\n"
	@printf "make push-$(ARTIFACTORY_REPO)-release\tPushes butler to $(ARTIFACTORY_PROD_HOST).\n"
	@printf "make push-butler-dockerhub\tPushes butler to DockerHub (If necessary).\n"
	@printf "make run\t\t\tRun butler on local system.\n"
	@printf "make start-prometheus\t\tRun a local prometheus instance for testing.\n"
	@printf "make stop-prometheus\t\tStop the local test prometheus instance.\n"
	@printf "make prometheus-logs\t\tTail the logs of the test prometheus instance.\n"

run:
	$(GO) run butler.go promfuncs.go -config.url http://git1.dev.or1.adobe.net/cgit/adobe-platform/ethos-monitoring/plain/oncluster -config.mustache-subs "ethos-cluster-id=ethos01-dev-or1" -config.scheduler-interval 10 -config.prometheus-host localhost

start-prometheus:
	@docker run --rm -it --name=prometheus -d -p 9090:9090 -v /opt/prometheus:/etc/prometheus prom/prometheus -config.file=/etc/prometheus/prometheus.yml -storage.local.path=/prometheus -storage.local.memory-chunks=104857

stop-prometheus:
	@docker stop prometheus

prometheus-logs:
	@docker logs -f prometheus
