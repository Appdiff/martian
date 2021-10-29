#!/bin/bash

# Fail fast
set -e

# Shorthand: fly -t carbon sp -p martian-proxy -c ci/pipeline.yml
fly --target carbon set-pipeline --pipeline martian-proxy --config ci/pipeline.yml
