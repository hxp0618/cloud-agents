#!/bin/sh

set -eu

script_directory=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
environment_file=${1:-"$script_directory/.env"}

docker compose --env-file "$environment_file" -f "$script_directory/docker-compose.yml" --profile bootstrap run --rm bootstrap
docker compose --env-file "$environment_file" -f "$script_directory/docker-compose.yml" run --rm migrate
identity_initialize=$(sed -n 's/^CLOUD_AGENTS_IDENTITY_INITIALIZE=//p' "$environment_file" | tail -n 1)
case "$identity_initialize" in
  1) docker compose --env-file "$environment_file" -f "$script_directory/docker-compose.yml" --profile identity-initialize run --rm identity-initialize ;;
  0) ;;
  *) echo "CLOUD_AGENTS_IDENTITY_INITIALIZE must be 0 or 1" >&2; exit 2 ;;
esac
docker compose --env-file "$environment_file" -f "$script_directory/docker-compose.yml" --profile tenant-bootstrap run --rm tenant-bootstrap
exec docker compose --env-file "$environment_file" -f "$script_directory/docker-compose.yml" up --build
