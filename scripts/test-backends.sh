#!/usr/bin/env bash
# Runs the whole suite against memory, sqlite, postgres and mongo. Starts its
# own containers on odd ports so it never collides with anything you already
# run, and removes them on exit.
set -euo pipefail
PG=keysmith-test-pg
MG=keysmith-test-mongo
cleanup() { docker rm -f "$PG" "$MG" >/dev/null 2>&1 || true; }
trap cleanup EXIT
cleanup
docker run -d --name "$PG" -e POSTGRES_PASSWORD=ks -e POSTGRES_DB=ks -p 55437:5432 postgres:16-alpine >/dev/null
docker run -d --name "$MG" -p 57037:27017 mongo:7 >/dev/null
for _ in $(seq 1 60); do
  docker exec "$PG" pg_isready -U postgres >/dev/null 2>&1 && break
  sleep 1
done
export KEYSMITH_TEST_PG_DSN="postgres://postgres:ks@localhost:55437/ks?sslmode=disable"
export KEYSMITH_TEST_MONGO_URI="mongodb://localhost:57037"
go test -count=1 ./... "$@"
