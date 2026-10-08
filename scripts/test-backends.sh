#!/usr/bin/env bash
# Runs the whole suite against memory, sqlite, postgres and mongo. Starts its
# own containers on odd ports so it never collides with anything you already
# run, and removes them on exit.
#
# The host ports sit below 49152 on purpose. macOS hands that range and above
# to outbound connections, so a browser can be holding one of them when the
# containers start. Override with KEYSMITH_TEST_PG_PORT and
# KEYSMITH_TEST_MONGO_PORT if these two are taken on your machine.
set -euo pipefail
PG=keysmith-test-pg
MG=keysmith-test-mongo
PG_PORT="${KEYSMITH_TEST_PG_PORT:-34437}"
MG_PORT="${KEYSMITH_TEST_MONGO_PORT:-34717}"
cleanup() { docker rm -f "$PG" "$MG" >/dev/null 2>&1 || true; }
trap cleanup EXIT
cleanup

# port_in_use succeeds when something already listens on the TCP port. lsof sees
# listeners on any address; the /dev/tcp probe is the fallback without it.
port_in_use() {
  if command -v lsof >/dev/null 2>&1; then
    lsof -nP -iTCP:"$1" -sTCP:LISTEN >/dev/null 2>&1
  else
    (exec 3<>"/dev/tcp/127.0.0.1/$1") 2>/dev/null
  fi
}
for spec in "postgres:$PG_PORT:KEYSMITH_TEST_PG_PORT" "mongo:$MG_PORT:KEYSMITH_TEST_MONGO_PORT"; do
  IFS=: read -r name port var <<<"$spec"
  if port_in_use "$port"; then
    echo "port $port is already in use, so the $name test container cannot bind it." >&2
    echo "Free it, or pick another with $var=<port> (keep it below 49152)." >&2
    exit 1
  fi
done

docker run -d --name "$PG" -e POSTGRES_PASSWORD=ks -e POSTGRES_DB=ks -p "$PG_PORT:5432" postgres:16-alpine >/dev/null
docker run -d --name "$MG" -p "$MG_PORT:27017" mongo:7 >/dev/null
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
export KEYSMITH_TEST_PG_DSN="postgres://postgres:ks@localhost:$PG_PORT/ks?sslmode=disable"
export KEYSMITH_TEST_MONGO_URI="mongodb://localhost:$MG_PORT"
go test -count=1 ./... "$@"
