#!/bin/bash

docker run \
  -e "PGDATABASE=identity_development" \
  -e "PGHOST=postgres" \
  -e "PGUSER=postgres" \
  -e "PGPASSWORD=sup" \
  -e "PGSSLMODE=disable" \
  -p "18883:18883" \
  --link postgres:postgres \
  -it simple_broker
