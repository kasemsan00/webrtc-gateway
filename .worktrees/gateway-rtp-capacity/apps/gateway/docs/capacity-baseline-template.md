# Capacity Validation Template

## Environment

- Instance shape: `8 vCPU / 16 GB RAM`
- Profile: `scripts/load-profile.json`

## Baseline Run

- Result file: `scripts/capacity/results/<timestamp>-baseline.json`
- First failing step:
- Dominant bottleneck:

## Improved Run

- Result file: `scripts/capacity/results/<timestamp>-improved.json`
- First failing step:
- Dominant bottleneck:

## Gate Evaluation

- Setup success rate >= 99%
- Setup latency p95 <= 2500 ms
- RTP loop p95 <= 20 ms
- CPU <= 75% avg5m
- Memory <= 80%
- WS queue full <= 1 / 1000 session-minutes
- Quality alarm window <= 15 s

## Recommendation

- Safe session ceiling:
- Safety margin applied:
- Open risks:
