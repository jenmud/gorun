#!/bin/bash
set -e
curl -sf https://binaries.nats.dev/nats-io/natscli/nats@latest | sh
mv nats $HOME/.local/bin/
