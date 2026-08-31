#!/bin/sh

set -eu

script_directory=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
environment_file=${1:-"$script_directory/.env"}

docker compose --env-file "$environment_file" -f "$script_directory/docker-compose.yml" --profile bootstrap run --rm bootstrap
docker compose --env-file "$environment_file" -f "$script_directory/docker-compose.yml" --profile tenant-bootstrap run --rm tenant-bootstrap
exec docker compose --env-file "$environment_file" -f "$script_directory/docker-compose.yml" up --build
