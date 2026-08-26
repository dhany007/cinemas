#!/bin/sh

set -eu

project_name="cinemas-migration-smoke-$$"

cleanup() {
    docker compose --project-name "$project_name" \
        -f compose.yaml \
        -f compose.migration-test.yaml \
        down --volumes --remove-orphans
}

trap cleanup EXIT INT TERM

docker compose --project-name "$project_name" \
    -f compose.yaml \
    -f compose.migration-test.yaml \
    up --abort-on-container-exit --exit-code-from migrate migrate
