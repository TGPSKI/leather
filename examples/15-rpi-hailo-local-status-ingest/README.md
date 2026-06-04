# 15-rpi-hailo-local-status-ingest

A real-value tiny endpoint ingest example.

This example collects deterministic local status evidence, ingests the snapshot as a Leather hide, then cures it into a compact operational artifact using a tiny local model endpoint.

The model does not inspect the machine. The shell snapshot does. The model only compresses bounded evidence into a readable digest.

## Run

```bash
make doctor
make run
```

## Flow

```text
scripts/collect-status.sh
  -> sample/status.snapshot.txt

leather ingest --config config.yaml --tannery tannery.yaml --kind local.status --source local --curing local-status-digest --queue default sample/status.snapshot.txt
  -> .state/hides/<id>/
  -> .state/queues/default.jsonl

leather serve --config config.yaml --tannery tannery.yaml
  -> local-status-digest curing
  -> local-status agent
  -> .state/artifacts/local-status-digest/<id>.json
```
