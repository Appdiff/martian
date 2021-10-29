#!/bin/bash

set -x
set -e

docker run -it --rm martian-proxy $@
