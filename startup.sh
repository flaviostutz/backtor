#!/bin/bash
set -e
set -x

echo "Starting backtor..."
backtor \
    --conductor-api-url=$CONDUCTOR_API_URL \
    --data-dir="$DATA_DIR" \
    --log-level=$LOG_LEVEL

