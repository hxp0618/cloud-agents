#!/bin/sh

set -eu

repository_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
export GOTOOLCHAIN=local
export GOFLAGS=-mod=readonly
export GOWORK="$repository_root/go.work"

case $(go version) in
  "go version go1.26.6 "*) ;;
  *)
    echo "Cloud Agents product checks require Go 1.26.6." >&2
    exit 1
    ;;
esac

test_packages() {
  module=$1
  packages=$2
  go -C "$repository_root/$module" test $packages
  go -C "$repository_root/$module" test -race $packages
  go -C "$repository_root/$module" vet $packages
  GOWORK=off go -C "$repository_root/$module" mod verify
}

sdk_packages=$(go -C "$repository_root/sdk/go" list ./...)
worker_packages=$(go -C "$repository_root/services/worker" list ./...)
# The production migrator uses cmd/cloud-agents-product-migrate and internal/localmigration.
# internal/migration is the historical Gate/evidence implementation, not a product package.
control_plane_packages=$(go -C "$repository_root/services/control-plane" list ./... | awk '$0 !~ /\/internal\/migration$/')

test_packages sdk/go "$sdk_packages"
test_packages services/worker "$worker_packages"
test_packages services/control-plane "$control_plane_packages"

go -C "$repository_root/services/worker" test -tags=localdev ./cmd/cloud-agents-worker
go -C "$repository_root/services/worker" test -race -tags=localdev ./cmd/cloud-agents-worker
go -C "$repository_root/services/worker" vet -tags=localdev ./cmd/cloud-agents-worker
go -C "$repository_root/services/control-plane" test -tags=localdev ./cmd/cloud-agents-control-plane
go -C "$repository_root/services/control-plane" test -race -tags=localdev ./cmd/cloud-agents-control-plane
go -C "$repository_root/services/control-plane" vet -tags=localdev ./cmd/cloud-agents-control-plane
