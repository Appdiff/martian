#!/bin/bash

set -x
set -e

REMOTE_TAG=""
if [ -n "$1" ]; then
  REMOTE_TAG="-t carbon-docker-dev.test.ai/carbon/martian-proxy:$1"
fi

docker build -t martian-proxy $REMOTE_TAG .

if [ -n "$1" ]; then
  docker push "carbon-docker-dev.test.ai/carbon/martian-proxy:$1"
fi
