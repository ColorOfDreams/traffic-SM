# GPS map-matching benchmark

Build the benchmark image from the repository root:

```powershell
docker build -f tools/gps-benchmark/Dockerfile -t traffic-gps-benchmark:dev .
```

Preview five JSON targets:

```powershell
docker run --rm `
  -v "${PWD}/data/gps-fake:/data:ro" `
  --entrypoint gps-vegeta-targets `
  traffic-gps-benchmark:dev `
  --input /data/fake_gps.csv `
  --max-drivers 5 `
  --events-per-driver 1
```

Run a 60-second attack. `--cycles=0` keeps producing unique event IDs and
monotonically increasing timestamps if Vegeta consumes the whole CSV:

```powershell
New-Item -ItemType Directory -Force data/benchmark-results

docker run --rm `
  --network traffic-system_traffic-network `
  -v "${PWD}/data/gps-fake:/data:ro" `
  -v "${PWD}/data/benchmark-results:/results" `
  -e RATE=1000/s `
  -e DURATION=60s `
  -e RUN_ID=mapmatch-1000 `
  -e TIME_SHIFT=24h `
  -e RESULT_FILE=/results/mapmatch-1000.bin `
  --entrypoint run-gps-benchmark `
  traffic-gps-benchmark:dev
```

Use a larger `TIME_SHIFT` for every consecutive run so timestamps never move
backwards relative to the trace-builder state.

For rates that exceed one generator's capacity, run multiple containers with
the same `SHARD_COUNT` and a unique `SHARD_INDEX` from zero to
`SHARD_COUNT - 1`. Drivers are disjoint between shards, so Kafka ordering per
driver is preserved. Split the desired total rate between the containers.

Print a report:

```powershell
docker run --rm `
  -v "${PWD}/data/benchmark-results:/results:ro" `
  --entrypoint vegeta `
  traffic-gps-benchmark:dev `
  report /results/mapmatch-1000.bin
```
