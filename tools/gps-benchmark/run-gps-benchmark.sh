#!/bin/sh
set -eu

GPS_INPUT="${GPS_INPUT:-/data/fake_gps.csv}"
TARGET_URL="${TARGET_URL:-http://location-service:8080/v1/gps-events}"
RATE="${RATE:-1000/s}"
DURATION="${DURATION:-60s}"
RUN_ID="${RUN_ID:-run-$(date +%s)}"
RESULT_FILE="${RESULT_FILE:-/results/mapmatch.bin}"
WORKERS="${WORKERS:-100}"
MAX_WORKERS="${MAX_WORKERS:-2000}"
MAX_BODY="${MAX_BODY:-256}"
TIME_SHIFT="${TIME_SHIFT:-0s}"
SHARD_INDEX="${SHARD_INDEX:-0}"
SHARD_COUNT="${SHARD_COUNT:-1}"

echo "GPS benchmark starting: rate=${RATE} duration=${DURATION} run_id=${RUN_ID}" >&2
echo "Timestamp shift: ${TIME_SHIFT}" >&2
echo "Driver shard: ${SHARD_INDEX}/${SHARD_COUNT}" >&2
echo "Target: ${TARGET_URL}" >&2
echo "Result: ${RESULT_FILE}" >&2

gps-vegeta-targets \
  --input "${GPS_INPUT}" \
  --url "${TARGET_URL}" \
  --cycles 0 \
  --run-id "${RUN_ID}" \
  --time-shift "${TIME_SHIFT}" \
  --shard-index "${SHARD_INDEX}" \
  --shard-count "${SHARD_COUNT}" \
| vegeta attack \
    -format=json \
    -lazy \
    -rate="${RATE}" \
    -duration="${DURATION}" \
    -workers="${WORKERS}" \
    -max-workers="${MAX_WORKERS}" \
    -max-body="${MAX_BODY}" \
    -name="${RUN_ID}" \
    -output="${RESULT_FILE}"

echo "GPS benchmark completed: rate=${RATE} duration=${DURATION} run_id=${RUN_ID}" >&2
