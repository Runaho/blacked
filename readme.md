<img src="https://github.com/user-attachments/assets/a54c8d22-77ba-4a05-9d86-aca811d1c1f9" alt="Blacked Logo" height="150px" />

<div align="center">
  <img src="https://img.shields.io/badge/Go-1.26+-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go 1.26+"/>
  <img src="https://img.shields.io/badge/License-MIT-blue?style=for-the-badge" alt="License: MIT"/>
  <img src="https://img.shields.io/badge/API-REST-green?style=for-the-badge" alt="API: REST"/>
  <img src="https://img.shields.io/badge/Version-0.3.0-4ADE80?style=for-the-badge" alt="Version 0.3.0"/>
</div>

<br>

**High-performance URL blacklist aggregator with multi-bloom filtering and scoring.**

Blacked collects threat intelligence from multiple sources (OISD, URLHaus, ThreatFox, AlienVault, AbuseIPDB, and more), decomposes every URL/IP across 6 bloom dimensions, and answers `is this URL/IP blocked?` in ~0.4ms.

<table>
  <tr>
    <td align="center"><h3>📡 Aggregation</h3><p><sub>Provider → Source pipeline with independent fetch, parse, and schedule per source. 12 active providers feeding <strong>1M+ entries</strong>.</sub></p></td>
    <td align="center"><h3>🧬 Bloom Engine</h3><p><sub>6-layer parallel check at ~0.4ms. One entry → one bloom type. First hit wins, parent-path cascade.</sub></p></td>
    <td align="center"><h3>📊 Scoring</h3><p><sub>Provider trust × depth weight. Single match uses trust directly. 5 levels: critical → informational.</sub></p></td>
    <td align="center"><h3>🏗️ Core</h3><p><sub>HTTP-agnostic `internal/query/` package. Testable standalone. Adapter pattern — zero framework lock-in.</sub></p></td>
  </tr>
</table>

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
| **Host Normalization** | Entry.Host = `url.Hostname()` — port stripped. Bloom keys and DB confirmation use same format, no mismatch |

---

## ⚡ Performance

| Metric | Value |
|:-------|:------|
| Bloom Check (P99) | **0.4 ms** |
| Full Hit (bloom + DB + score) | **5–15 ms** |
| CPU Usage (idle, 1M entries) | **~2%** |
| Heap (idle) | **~420 MB** |
| Sync Alloc | **~1.73 GB** |
| Sync Duration (12 providers, 1M entries) | **~30 s** |
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
│           Source Layer                  │
│  (Fetcher + Parser per source URL)      │
└────────────────┬────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────┐
│      Pond Collector (batched writer)    │
│  ┌──────────┐  ┌──────────┐  ┌───────┐  │
│  │  SQLite  │  │  Badger  │  │ Bloom │  │
│  │  (WAL)   │  │  Cache   │  │  Sets │  │
│  └──────────┘  └──────────┘  └───────┘  │
└─────────────────────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────┐
│         Query Core (internal/query/)    │
│   Check (bloom only) → Hit (full)       │
│   ┌──────────┐  ┌──────────┐            │
│   │ Scorer   │  │ Adapter  │            │
│   └──────────┘  └──────────┘            │
└────────────────┬────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────┐
│      REST API (Echo)                    │
│  /api/v1/check  /api/v1/hit             │
│  /api/v1/bulk-check  /api/v1/bulk-hit   │
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
              ├── Host:      cdn.evil.com       → BloomHost      │
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
cp .env.toml.copy .env.toml
# Edit to suit your environment

# Run the server
go run . serve
```

The server starts at `http://localhost:8088`.

### Supported Providers

| Provider | Type | Entries | Schedule | Description |
|----------|------|---------|----------|-------------|
| **oisd-big** | Domain | ~440K | Daily | Comprehensive domain blacklist |
| **oisd-nsfw** | Domain | ~330K | Daily | Adult/malware domains |
| **urlhaus-online** | URL/IP | ~76K | 2h | Active malware URLs |
| **threatfox-online** | IOC | ~104K | 2h | ThreatFox IOC feed |
| **alienvault** | IOC | ~500K+ | 6h | AlienVault OTX pulses |
| **rtbh-turkey** | IP | ~63K | 30min | Turkish government blocklist |
| **blocklist-de** | IP | ~24K | 15min | Multi-category attack IPs |
| **cins-army** | IP | ~15K | 30min | Active scanner/probe IPs |
| **abuseipdb** | IP | ~10K | Daily | AbuseIPDB blacklist |
| **greensnow** | IP | ~6K | 6h | Attack IPs |
| **tor-exit-nodes** | IP | ~1.3K | 6h | Tor exit nodes |
| **emerging-threats** | IP | ~500 | 12h | Compromised IPs |
| **openphish-feed** | URL | ~300 | 4h | Phishing URLs |
| **phishtank-online-valid** | URL | - | 6h | PhishTank (API key required, disabled by default) |

**Total: 1M+ entries** (domains + IPs + URLs combined)

### CLI

```bash
# Process all providers immediately
go run . process

# Query a URL
go run main.go query --url "https://evil.com/path"

# JSON output
go run main.go query --url "https://evil.com" --json
```

---

## 📡 REST API

### Core Endpoints

| Endpoint | Method | Description | Latency |
|:---------|:-------|:------------|:--------|
| `/api/v1/check?url=` | GET | Bloom-only check — fast negative | ~0.4 ms |
| `/api/v1/hit?url=` | GET | Bloom + DB confirmation + scorer — confidence + level + matches | ~5–15 ms |
| `/api/v1/bulk-check` | POST | Batch bloom check (up to N URLs) | ~0.4 ms × N |
| `/api/v1/bulk-hit` | POST | Batch bloom + DB + scorer | ~5–15 ms × N |

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
    "source_id": "urlhaus-online"
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
port = 8088
host = "localhost"

[Cache]
use_bloom = true
badger_path = ""

[Collector]
batch_size = 1000
cron_schedule = "0 0 * * *"

# Domain/URL providers
[providers.oisd-big]
enabled = true
source_url = "https://big.oisd.nl/domainswild2"
cron = "0 6 * * *"
category = "blocklist"
parser_workers = 4
parser_batch_size = 1000

[providers.oisd-nsfw]
enabled = true
source_url = "https://nsfw.oisd.nl/domainswild"
cron = "22 6 * * *"
category = "nsfw"
parser_workers = 4
parser_batch_size = 1000

[providers.urlhaus-online]
enabled = true
source_url = "https://urlhaus.abuse.ch/downloads/text/"
cron = "15 */2 * * *"
category = "malware"
parser_workers = 4
parser_batch_size = 1000
timeout = "90s"
max_retries = 3
circuit_breaker = true

[providers.openphish-feed]
enabled = true
source_url = "https://openphish.com/feed.txt"
cron = "30 */4 * * *"
category = "phishing"
parser_workers = 4
parser_batch_size = 1000

[providers.phishtank-online-valid]
enabled = false
source_url = "https://data.phishtank.com/data/{api_key}/online-valid.json"
api_key = ""  # Required if enabled
cron = "45 */6 * * *"
category = "phishing"

# IOC providers
[providers.threatfox-online]
enabled = true
api_key = "your-threatfox-api-key"
source_url = "https://threatfox-api.abuse.ch/v2/files/exports/{token}/recent.json"
dump_source_url = "https://threatfox-api.abuse.ch/v2/files/exports/{token}/full.json.zip"
cron = "0 */2 * * *"
category = "threat_intel"
parser_workers = 4
parser_batch_size = 1000

[providers.alienvault]
enabled = true
url = "https://otx.alienvault.com/api/v1/pulses/subscribed"
api_key = "your-alienvault-api-key"
cron = "0 */6 * * *"
category = "threat_intel"
parser_workers = 4
parser_batch_size = 1000

# IP providers
[providers.abuseipdb]
enabled = true
url = "https://api.abuseipdb.com/api/v2/blacklist"
api_key = "your-abuseipdb-api-key"
confidence_minimum = 90
limit = 10000
cron = "0 0 * * *"
category = "abuse"
parser_workers = 4
parser_batch_size = 1000

[providers.blocklist-de]
enabled = true
source_url = "https://lists.blocklist.de/lists/all.txt"
cron = "0 */15 * * *"
category = "attacker"
parser_workers = 4
parser_batch_size = 1000

[providers.cins-army]
enabled = true
source_url = "https://cinsscore.com/list/ci-badguys.txt"
cron = "*/30 * * * *"
category = "scanner"
parser_workers = 4
parser_batch_size = 1000

[providers.rtbh-turkey]
enabled = true
source_url = "https://list.rtbh.com.tr/output.txt"
cron = "*/30 * * * *"
category = "government-feed"
parser_workers = 4
parser_batch_size = 1000

[providers.greensnow]
enabled = true
source_url = "https://blocklist.greensnow.co/greensnow.txt"
cron = "0 */6 * * *"
category = "attacker"
parser_workers = 4
parser_batch_size = 1000

[providers.tor-exit-nodes]
enabled = true
source_url = "https://check.torproject.org/torbulkexitlist"
cron = "0 */6 * * *"
category = "tor"
parser_workers = 4
parser_batch_size = 1000

[providers.emerging-threats]
enabled = true
source_url = "https://rules.emergingthreats.net/blockrules/compromised-ips.txt"
cron = "0 */12 * * *"
category = "compromised"
parser_workers = 4
parser_batch_size = 1000
```

**All provider settings come from `.env.toml` — zero hard-coded URLs, crons, or categories.** API keys are never committed to code; they live in the `api_key` field of the provider block or are injected via environment variables.

---

## 📦 Adding a Provider

Each provider is a Go package in `features/providers/`. Add a new TOML block in `.env.toml`, then implement a constructor:

```go
// features/providers/myprovider/myprovider.go
func NewMyProvider(cfg *config.Config, collyClient *colly.Collector) base.Provider {
    const providerName = "myprovider"

    opts, ok := cfg.Providers[providerName]
    if !ok || opts == nil {
        opts = &config.ProviderOptions{} // defaults kick in
    }
    if opts.Enabled != nil && !*opts.Enabled { return nil }

    sourceURL := opts.SourceURL
    if sourceURL == "" {
        sourceURL = "https://example.com/feed.txt" // built-in default
    }
    cron := opts.Cron
    if cron == "" {
        cron = "0 */6 * * *" // built-in default
    }
    category := opts.Category
    if category == "" {
        category = "blocklist" // built-in default
    }

    workers := opts.ParserWorkers
    if workers <= 0 { workers = 4 }
    batchSize := opts.ParserBatchSize
    if batchSize <= 0 { batchSize = 1000 }

    client := base.BuildCollyClientForProvider(collyClient, opts)

    parseFunc := func(data io.Reader, collector entry_collector.Collector) error {
        return base.ParseLinesParallel(data, collector, providerName,
            workers, batchSize, func(line, processID string) (*entries.Entry, error) {
                // ... parse logic ...
            })
    }

    provider := base.NewBaseProvider(providerName, sourceURL, category, client, parseFunc)
    provider.SetCronSchedule(cron).Register()
    return provider
}
```

```toml
# .env.toml
[providers.myprovider]
enabled = true
source_url = "https://example.com/feed.txt"
cron = "0 */6 * * *"
category = "malware"
parser_workers = 4
parser_batch_size = 1000
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
├── providers/           # Provider system (14 providers)
│   ├── oisd/            # OISD Big & NSFW domain lists
│   ├── urlhaus/         # URLHaus malware URLs
│   ├── openphish/       # OpenPhish phishing feed
│   ├── phishtank/       # PhishTank phishing feed
│   ├── threatfox/       # ThreatFox IOC feed
│   ├── alienvault/      # AlienVault OTX pulses
│   ├── abuseipdb/       # AbuseIPDB blacklist
│   ├── blocklistde/     # Blocklist.de attack IPs
│   ├── cinsarmy/        # CINS Army scanner IPs
│   ├── rtbh/            # RTBH Turkey
│   ├── greensnow/       # GreenSnow attack IPs
│   ├── torexit/          # Tor exit nodes
│   └── emergingthreats/  # Emerging Threats compromised IPs
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
