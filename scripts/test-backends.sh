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
# pg_isready answers during the image's temporary init server, which then
# restarts. A real query over TCP only succeeds once the final server is up.
ready=""
for _ in $(seq 1 60); do
  if docker exec "$PG" psql -h 127.0.0.1 -U postgres -d ks -c 'select 1' >/dev/null 2>&1; then
    ready=1
    break
  fi
  sleep 1
done
if [ -z "$ready" ]; then
  echo "postgres did not become ready after 60 tries" >&2
  exit 1
fi
ready=""
for _ in $(seq 1 60); do
  if [ "$(docker exec "$MG" mongosh --quiet --eval 'db.runCommand({ping:1}).ok' 2>/dev/null | tr -d '[:space:]')" = "1" ]; then
    ready=1
    break
  fi
  sleep 1
done
if [ -z "$ready" ]; then
  echo "mongo did not become ready after 60 tries" >&2
  exit 1
fi
export KEYSMITH_TEST_PG_DSN="postgres://postgres:ks@localhost:55437/ks?sslmode=disable"
export KEYSMITH_TEST_MONGO_URI="mongodb://localhost:57037"
go test -count=1 ./... "$@"
