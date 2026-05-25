# Cache Layer Architecture

Blacked uses two distinct cache layers with different characteristics and responsibilities.

## Layers Overview

| Layer | Storage | Persistence | Use Case |
|---|---|---|---|
| **BloomFilter** | In-memory | Lost on restart | Fast false-positive filtering (~6.8% FP rate) |
| **Badger** | Disk (LSM tree) | Survives restarts | Persistent key→IDs lookup |

## BloomFilter (In-Memory Probabilistic Cache)

**Package:** `features/cache/bloom.go`

A bloom filter provides O(1) probabilistic checking. It can tell you "possibly in set" or "definitely not in set" with a configurable false-positive rate (default 1%).

### Characteristics
- **Memory:** All entries stored as bitset; ~1.2MB per 1M entries at 1% FP
- **Speed:** Fastest path — single hash per key, no I/O
- **Persistence:** None. Populated on startup from DB entries (~2-3s for 630K entries)
- **Accuracy:** ~6.8% false-positive rate across 7 test sets; confirmed by DB query

### Use in query pipeline
```
/check  → BloomFilter.Test() → [maybe] → DB lookup
/hit    → BloomFilter.Test() → [maybe] → DB lookup + scoring
```
Bloom filter alone is used for `/check` (DB confirmation optional). For `/hit`, bloom positives are always confirmed against SQLite before scoring.

## Badger (Persistent Disk Cache)

**Package:** `features/cache/`

Badger is an embeddable key-value database based on LSM trees. It provides persistent caching of URL→IDs mappings.

### Characteristics
- **Storage:** Disk (WAL + SSTable files in `features/cache/badger_provider/`)
- **Persistence:** Survives restarts; survives across process crashes (WAL)
- **Speed:** Disk I/O, but hot keys stay in block cache
- **Capacity:** Limited only by disk space (unlike bloom's in-memory limits)

### Cache sync lifecycle

PondCollector manages the sync between bloom and badger:

1. **Entry collected** → submitted to buffer
2. **Buffer flushed** → batch written to DB + bloom populated
3. **Cache sync triggered** (on-demand) → badger updated from DB via `Iterate()`

```
Provider sync → PondCollector.Submit() → DB write
                               ↓
                      BloomManager.PopulateEntry()
                               ↓
                      PondCollector.ScheduleCacheSync()
                               ↓
                      BadgerProvider.SyncFromDB()
```

## Cache Flow Diagram

```
URL submitted
     │
     ▼
┌─────────────────┐
│  BloomFilter    │ ← In-memory, fast, probabilistic
│  (blacks filter)│   First check in query pipeline
└────────┬────────┘
         │ bloom positive?
         ▼
┌─────────────────┐
│     SQLite      │ ← Persistent, authoritative source
│  (entries DB)   │   Confirms all bloom positives
└────────┬────────┘
         │ confirmed
         ▼
┌─────────────────┐
│     Badger      │ ← Persistent disk cache, populated
│ (badger_provider)│  on cache sync from DB
└─────────────────┘
```

## Responsibilities

| Component | Responsibility |
|---|---|
| `BloomFilter` | Fast in-memory pre-filter for incoming queries. Reduces DB load by ~93%. |
| `BadgerProvider` | Persistent URL→IDs cache, survives restarts. Populated asynchronously via cache sync. |
| `PondCollector` | Orchestrates DB writes, bloom population, and badger sync. |
| `BloomManager` | Manages multiple bloom filter sets (by source). Lives inside PondCollector. |

## Startup Bootstrap

On server start, `PondCollector` bootstraps the bloom filter from existing DB entries:

```go
// Direct SQL query — faster than streaming
rows, _ := db.QueryContext(ctx,
    `SELECT source, domain, host, path, raw_query
     FROM entries WHERE deleted_at IS NULL`)
for rows.Next() {
    // Populate BloomManager
}
```

This takes ~2-3s for 630K entries in the background, during which `/check` queries may return 204 (no bloom hit yet).

## Configuration

Cache type is configured via `config.yaml`:

```yaml
cache:
  cache_type: "badger"  # Only badger currently supported
  badger:
    path: "./features/cache/badger_provider"
```

The bloom filter capacity is set in `features/entry_collector/pond_collector.go`:

```go
bloomMgr := bloom.NewBloomManager(1_000_000)  // 1M expected entries
```