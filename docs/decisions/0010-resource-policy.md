# 0010. Resource policy: no idle polling, adaptive read-ahead, pooled buffers

- **Status:** Accepted
- **Date:** 2026-10-16

## Context
The user's top priority is avoiding overconsumption. Measurements showed polling every 3 s (and opening every USB device when no phone was connected), fixed 4 MiB read-ahead (200 MB over USB for 40 thumbnail reads), and per-transfer buffer allocations.

## Decision
Wake only on libusb hotplug events (a slow safety check only while no phone is connected). Read-ahead starts at 128 KiB and doubles on sequential reads up to 8 MiB. Pool buffers 64 KiB-16 MiB (`internal/bufpool`); zero-copy partial reads. The activity hub sends nothing while idle.

## Alternatives considered
| Option | Why not |
|---|---|
| Keep polling | kept the phone's MTP service awake and woke every USB device |

## Consequences
Idle: 0 MTP transactions, ~10 MB RSS. Thumbnail-style reads 3x faster with 5x less USB traffic. Measure before/after with real transfers for any performance change.
