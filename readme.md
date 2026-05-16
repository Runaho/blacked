# Blacked 🖤

<img src="https://github.com/user-attachments/assets/a54c8d22-77ba-4a05-9d86-aca811d1c1f9" alt="Blacked Logo" height="150px" />

<div align="center">
  <img src="https://img.shields.io/badge/Go-1.26+-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go 1.26+"/>
  <img src="https://img.shields.io/badge/License-MIT-blue?style=for-the-badge" alt="License: MIT"/>
  <img src="https://img.shields.io/badge/API-REST-green?style=for-the-badge" alt="API: REST"/>
  <img src="https://img.shields.io/badge/Version-0.3.0-4ADE80?style=for-the-badge" alt="Version 0.3.0"/>
</div>

<br>

**High-performance URL blacklist aggregator with multi-bloom filtering and scoring.**

Blacked collects threat intelligence from multiple sources (OISD, URLHaus, OpenPhish, PhishTank), decomposes every URL across 6 bloom dimensions, and answers `is this URL blocked?` in ~0.4ms — with confidence scoring, source attribution, and parallel first-hit-wins bloom checking.

---

## ✨ Features

| Capability | Detail |
|:-----------|:-------|
| **Multi-Source Aggregation** | Provider → Source hierarchy with independent fetch/parse pipelines |
| **Parallel Bloom Engine** | 6 bloom layers (Domain → Host → HostPath → File → FullURL → IP), checked concurrently — first hit wins |
| **Cascading Parent Match** | `/a/b/c/file.exe` matches `/a` or `/a/b` via parent-path traversal at check time |
| **Scoring & Levels** | Provider trust × depth weight → 5 confidence levels (critical → informational) |
| **Schedule-Aware Cache** | Parametric TTL per source/provider, cron-triggered invalidation, app-restart resilience |
| **Dual API** | Bloom-only check (~0.4ms) and full hit (bloom + DB + score, ~5-15ms) |
| **HTTP Agnostic Core** | `internal/query/` package decoupled from Echo, testable standalone |
| **Built-in Metrics** | Prometheus endpoints, execution tracing, pprof profiling |
| **No Legacy** | Greenfield schema, clean-slate policy — zero backward compatibility debt |

---

## ⚡ Performance

| Metric | Value |
|:-------|:------|
| Bloom Check (P99) | **0.4 ms** |
| Full Hit (bloom + DB + score) | **5–15 ms** |
| CPU Usage (idle, 820K entries) | **1.28%** |
| Heap (idle) | **101 MB** |
| Sync Alloc (before perf fixes) | 2.36 GB → **~1.73 GB** (−628 MB) |
| Sync Duration (3 providers, 826K entries) | **~109 s** |
| E2E Tests | **14 / 14** · **0.59 s** · No network calls |

---

## 🏗️ Architecture

```
┌──────────┐   ┌──────────┐   ┌──────────┐
│ Provider │   │ Provider │   │ Provider │
│  (OISD)  │   │ (URLHaus)│   │(OpenPhish)│
└────┬─────┘   └────┬─────┘   └────┬─────┘
     │              │              │
     ▼              ▼              ▼
┌─────────────────────────────────────────┐
│           Source Layer                   │
│  (Fetcher + Parser per source URL)      │
└────────────────┬────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────┐
│      Pond Collector (batched writer)    │
│  ┌──────────┐  ┌──────────┐  ┌───────┐ │
│  │  SQLite  │  │  Badger  │  │ Bloom │ │
│  │  (WAL)   │  │  Cache   │  │  Sets │ │
│  └──────────┘  └──────────┘  └───────┘ │
└─────────────────────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────┐
│         Query Core (internal/query/)    │
│   Check (bloom only) → Hit (full)       │
│   ┌──────────┐  ┌──────────┐           │
│   │ Scorer   │  │ Adapter  │           │
│   └──────────┘  └──────────┘           │
└────────────────┬────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────┐
│      REST API (Echo)                    │
│  /api/v1/check  /api/v1/hit            │
│  /api/v1/bulk-check  /api/v1/bulk-hit  │
└─────────────────────────────────────────┘
```

### Bloom Check Chain (Parallel, First Hit Wins)

```
Check URL "cdn.evil.com/malware/exploit.php?ref=bad"
    │
    ▼
ParseURL → GenerateKeys()
    │
    ├── Domain:    evil.com           → BloomDomain  ──┐
    ├── Host:      cdn.evil.com       → BloomHost     │
    ├── HostPath:  cdn.evil.com/ma... → BloomHostPath  ├── PARALLEL
    ├── File:      exploit.php        → BloomFile      │   First Hit
    ├── FullURL:   ...exploit.php?ref → BloomFullURL  ─┘   Cancels All
    └── IP:        103.224.212.251    → BloomIP
                                           │
                                           ▼
                          ┌──────────────────┴──────────────┐
                          ▼                                 ▼
               ✔ HIT → 200 OK                           ❌ MISS → 204
          { type: "file",                            No Content
            source: "oisd",
            key: "exploit.php",
            confidence: 0.85,
            level: "high" }
```

---

## 🚀 Quick Start

### Prerequisites

- Go 1.26+
- Git

### Setup

```bash
git clone https://github.com/runaho/blacked.git
cd blacked

# Download dependencies
go mod download

# Configure
cp .env.toml.example .env.toml
# Edit to suit your environment

# Run the server
go run . serve
```

The server starts at `http://localhost:8082`.

### CLI

```bash
# Process all providers immediately
go run . process

# Process specific provider
go run . process --provider OISD_BIG

# Query a URL
go run main.go query --url "https://evil.com/path"

# JSON output
go run main.go query --url "https://evil.com" --json
```

---

## 📡 REST API (V2)

### Core Endpoints

| Endpoint | Method | Description | Latency |
|:---------|:-------|:------------|:--------|
| `/api/v1/check?url=` | GET | Bloom-only check — fast negative | ~0.4 ms |
| `/api/v1/hit?url=` | GET | Full check — bloom + DB + score | ~5–15 ms |
| `/api/v1/bulk-check` | POST | Batch bloom check (up to N URLs) | ~0.4 ms × N |
| `/api/v1/bulk-hit` | POST | Batch full check | ~5–15 ms × N |

### Responses

**Hit (200)** — URL is blocked:
```json
{
  "url": "https://cdn.evil.com/malware/exploit.php",
  "blocked": true,
  "confidence": 0.85,
  "level": "high",
  "matches": [{
    "type": "full_url",
    "key": "cdn.evil.com/malware/exploit.php",
    "source_id": "URLHAUS"
  }]
}
```

**Miss (204)** — URL is clean (or missing `url` parameter):
```
No Content
```

---

## ⚙️ Configuration

Blacked uses `.env.toml` (TOML format). Key sections:

```toml
[APP]
environment = "development"  # or "production"
log_level = "info"

[Server]
port = 8082
host = "localhost"

[Cache]
use_bloom = true
cache_type = "badger"

[Collector]
concurrency = 10
batch_size = 100
store_responses = true
store_path = "./responses"

[Provider]
enabled_providers = []              # empty = all enabled
run_at_startup = true
max_concurrent_providers = 0        # 0 = unlimited

# Per-provider cron schedules
# [Provider.provider_crons]
# OISD_BIG = "0 6 * * *"
# URLHAUS = "15 */2 * * *"
```

---

## 📦 Adding a Source

Each source needs a Fetcher (how to retrieve data) and a Parser (how to interpret it). Use the `features/sources/` package:

```go
// 1. Create a parser
func parseMyFormat(r io.Reader, entryChan chan<- *entries.Entry) error {
    scanner := bufio.NewScanner(r)
    for scanner.Scan() {
        line := scanner.Text()
        if strings.HasPrefix(line, "#") { continue }
        entry := &entries.Entry{
            SourceURL: line,
            Source:    "my-source",
            Category:  "malware",
        }
        entryChan <- entry
    }
    return scanner.Err()
}

// 2. Register in the source registry
sources.Register(Source{
    ID:         "my-source",
    ProviderID: "my-provider",
    Name:       "My Blacklist Source",
    SourceURL:  "https://example.com/feed.txt",
    SourceType: SourceTypeFlat,
    Enabled:    true,
    Parser:     parseMyFormat,
    Fetcher:    NewHTTPFetcher(),
})
```

---

## 🧪 Testing

```bash
# All unit and integration tests
go test ./... -count=1 -timeout 120s

# E2E bloom-aware tests (no network calls)
go test -tags=e2e ./features/e2e/... -v -timeout 60s

# Performance benchmarks
go test -bench=. ./features/web/handlers/benchmark/...
```

### E2E Test Coverage (14 subtests)

| # | Test | What it verifies |
|---|------|-----------------|
| 1 | DomainBloom | Domain-level match |
| 2 | HostBloom | Exact host match |
| 3 | HostPathBloom | Path-level match |
| 4 | ParentPathBloom | Parent path traversal (`/a` → `/a/b/c`) |
| 5 | FileBloom | File name match (`.exe`) |
| 6 | FullURLBloom | File + query match; different query = miss |
| 7 | IPBloom | IP bloom populate |
| 8 | FirstHitWinsDomain | Domain wins over HostPath on same URL |
| 9 | CleanMiss | Clean URL → 204 |
| 10–12 | HitEndpoint, HitClean, EmptyURL | Hit response, clean hit, empty param |
| 13–14 | BulkCheck, BulkHit | Batch endpoints |

---

## 🧬 Bloom Engine (Deep Dive)

### One Entry → One Bloom Type

Each blacklist entry goes into exactly **one** bloom set — determined by what the source provides:

| Source provides | Bloom type | Key | Example |
|:----------------|:-----------|:----|:--------|
| `evil.com` | Domain | `evil.com` | Covers all subdomains |
| `cdn.evil.com` | Host | `cdn.evil.com` | Exact subdomain |
| `cdn.evil.com/malware/` | HostPath | `cdn.evil.com/malware` | Folder-level block |
| `exploit.php` | File | `exploit.php` | File name, any path |
| `cdn.evil.com/exploit.php?ref=x` | FullURL | `cdn.evil.com/exploit.php?ref=x` | Exact request |
| `103.224.212.251` | IP | `103.224.212.251` | IP address |

### First Hit Wins

At check time, **all 6 bloom sets are queried in parallel goroutines**. The first `true` response cancels the rest via `context.Cancel()`. Bloom `Test()` is O(1), so goroutine overhead is negligible (~50 ns).

### Parent Path Matching

```
Check: cdn.x.com/a/b/c/file.exe
Generate HostPath keys (shallowest → deepest):
  /a
  /a/b
  /a/b/c

If source blacklisted cdn.x.com/a/b → HIT via parent path
```

---

## 📊 Scoring

**Single match**: confidence = provider trust score directly. A domain from a trusted source should reflect that trust — not be penalized for being "shallow."

**Multiple matches** (2+ bloom layers hit): depth weights are used to weigh matches against each other:

```
confidence = Σ(trust_score × depth_weight) / Σ(trust_score)
```

| Level | Score Range |
|:------|:------------|
| Critical | ≥ 0.90 |
| High | ≥ 0.70 |
| Medium | ≥ 0.50 |
| Low | ≥ 0.25 |
| Informational | < 0.25 |

Depth weights: Domain 0.3 · Host 0.5 · HostPath 1.0 · File 0.7 · FullURL 1.5 · IP 0.8

---

## 📁 Project Structure

```
features/
├── bloom/               # Multi-Bloom Engine (types, manager, URL parser)
├── cache/               # BadgerDB cache layer
├── entries/             # Entry model, repository, services
├── entry_collector/     # Pond collector (batch writer + cache sync)
├── providers/           # Provider system (OISD, URLHaus, OpenPhish, ...)
├── sources/             # Provider → Source decoupling (Fetcher, Parser, registry)
├── tests/               # Integration tests
├── web/                 # Echo handlers, routes, middleware
└── e2e/                 # Bloom-aware E2E tests (no network)

internal/
├── collector/           # Prometheus metrics collector
├── colly/               # Colly HTTP client wrapper
├── config/              # TOML-based configuration
├── db/                  # SQLite connection pool (read/write split), migrations
├── db/models/           # DB models (Provider, Source, Entry)
├── logger/              # Zerolog logger setup
├── query/               # HTTP-agnostic query core (service, scorer, types)
├── runner/              # gocron scheduler + provider executor
├── telemetry/           # OTLP tracing setup
├── testutil/            # Test helpers (DB, collector init)
├── tracing/             # Execution tracing
└── utils/               # Response cache, utilities
```

---


## 📜 License

MIT — see [LICENSE](LICENSE).

---

<div align="center">
  <sub>Built with ❤️ for better cybersecurity</sub>
</div>
