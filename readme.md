# 🛡️ Blacked

<div align="center">
  <img src="https://img.shields.io/badge/Go-1.24+-00ADD8?style=for-the-badge&logo=go&logoColor=white" alt="Go 1.24+"/>
  <img src="https://img.shields.io/badge/License-MIT-blue?style=for-the-badge" alt="License: MIT"/>
  <img src="https://img.shields.io/badge/API-REST-green?style=for-the-badge" alt="API: REST"/>
</div>

<br>

**Blacked** is a high-performance blacklist aggregator and query service built in Go. It efficiently collects blacklist data from various online sources, stores it using modern caching techniques, and provides blazing-fast query capabilities via both CLI and Web API interfaces.

## ✨ Key Features

<table>
  <tr>
    <td>
      <h3>🔄 Multi-Source Aggregation</h3>
      <p>Automatically fetches data from multiple blacklist sources including OISD, URLHaus, OpenPhish, and PhishTank with an extensible provider system for easy additions.</p>
    </td>
    <td>
      <h3>⚡ High-Performance Caching</h3>
      <p>Uses Bloom filters for ultra-fast negative lookups and BadgerDB for optimized key-value storage, ensuring millisecond-level response times.</p>
    </td>
  </tr>
  <tr>
    <td>
      <h3>🔍 Smart Query System</h3>
      <p>Supports multiple query types (exact URL, host, domain, path) with an intelligent cascading query strategy for comprehensive results.</p>
    </td>
    <td>
      <h3>📊 Built-in Metrics</h3>
      <p>Includes Prometheus metrics endpoints and performance benchmarking tools to monitor and optimize your deployment.</p>
    </td>
  </tr>
</table>

## 📋 Table of Contents

- [Installation](#-installation)
- [Configuration](#-configuration)
- [Usage](#-usage)
  - [CLI Commands](#cli-commands)
  - [REST API](#rest-api)
- [Adding New Providers](#-adding-new-providers)
- [Deployment](#-deployment)
- [Contributing](#-contributing)
- [License](#-license)

## 🚀 Installation

### Prerequisites

- Go 1.24 or higher
- Git

### Setup

```bash
# Clone the repository
git clone https://github.com/runaho/blacked.git
cd blacked

# Download dependencies
go mod download

# Configure the application
# Either copy the example config or create a new one
cp .env.toml.example .env.toml
# Edit according to your needs
```

## ⚙️ Configuration

Blacked is configured via a `.env.toml` file in the project root. You can also use a `.env` file or environment variables.

### Key Configuration Sections

```toml
[APP]
environment = "development" # or "production"
log_level = "info"          # debug, info, warn, error

[Server]
port = 8082
host = "localhost"

[Cache]
# For persistent storage:
# badger_path = "./badger_cache"
in_memory = true    # Use in-memory BadgerDB
use_bloom = true    # Enable Bloom filter for faster lookups

[Provider]
# Optionally limit enabled providers
# enabled_providers = ["OISD_BIG", "URLHAUS"]

# Override default schedules if needed
# [Provider.provider_crons]
# OISD_BIG = "0 7 * * *"  # Run OISD at 7 AM UTC
```

## 🖥️ Usage

### Running the Service

```bash
# Start the web server and scheduler
go run main.go serve

# Or with the built binary
./blacked serve
```

The server will start on `http://localhost:8082` by default (configurable in `.env.toml`).

### CLI Commands

Blacked includes a robust CLI for direct interaction:

```bash
# Process all providers immediately
go run main.go process

# Process specific providers only
go run main.go process --provider OISD_BIG --provider URLHAUS

# Query if a URL is blacklisted
go run main.go query --url "http://suspicious-site.com/path"

# Query with specific match type
go run main.go query --url "suspicious-site.com" --type domain

# Get query results as JSON
go run main.go query --url "http://suspicious-site.com" --json

# Get detailed help
go run main.go --help
```

### REST API

Blacked provides a comprehensive REST API for integration:

#### Core Endpoints

| Endpoint | Method | Description | Example |
|----------|--------|-------------|---------|
| `/entry` | GET | Quick check if a URL is blacklisted | `/entry?url=example.com` |
| `/entry/{id}` | GET | Get details for a specific entry by ID | `/entry/550e8400-e29b-41d4-a716-446655440000` |
| `/entry/search` | POST | Advanced search with query options | `{"url": "example.com", "query_type": "domain"}` |
| `/provider/process` | POST | Trigger provider processing | `{"providers_to_process": ["URLHAUS"]}` |
| `/benchmark/query` | POST | Benchmark query performance | `{"urls": ["example.com"], "iterations": 100}` |

#### Example Queries

```bash
# Check if a URL is blacklisted (fast path)
curl "http://localhost:8082/entry?url=http%3A%2F%2Fsuspicious-site.com"

# Comprehensive search
curl -X POST -H "Content-Type: application/json" \
     -d '{"url": "suspicious-site.com", "query_type": "domain"}' \
     http://localhost:8082/entry/search

# Trigger processing for specific providers
curl -X POST -H "Content-Type: application/json" \
     -d '{"providers_to_process": ["URLHAUS", "PHISHTANK"]}' \
     http://localhost:8082/provider/process
```

## ➕ Adding New Providers

Adding a new blacklist provider is straightforward:

1. Create a new directory for your provider: `features/providers/myprovider/`
2. Implement the provider interface:

```go
package myprovider

import (
    "blacked/features/entries"
    "blacked/features/providers/base"
    "blacked/internal/config"
    "io"
    
    "github.com/gocolly/colly/v2"
    "github.com/google/uuid"
    "github.com/rs/zerolog/log"
)

func NewMyProvider(settings *config.CollectorConfig, collyClient *colly.Collector) base.Provider {
    const (
        providerName = "MY_PROVIDER"
        providerURL  = "https://example.com/blacklist.txt"
        cronSchedule = "0 */6 * * *" // Every 6 hours
    )
    
    // Define how to parse provider data
    parseFunc := func(data io.Reader) ([]entries.Entry, error) {
        // Parse the data format specific to this provider
        // Return slice of entries.Entry
        // ...
    }
    
    // Create and register the provider
    provider := base.NewBaseProvider(
        providerName,
        providerURL,
        settings,
        collyClient,
        parseFunc,
    )
    
    provider.
        SetCronSchedule(cronSchedule).
        Register()
        
    return provider
}
```

3. Add your provider to `features/providers/main.go`:

```go
func NewProviders() (Providers, error) {
    // ...existing code...
    
    var providers = Providers{
        // ...existing providers...
        myprovider.NewMyProvider(&cfg.Collector, cc),
    }
    
    // ...existing code...
}
```

## 📦 Deployment

### Using Docker

```dockerfile
FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY . .
RUN go mod download
RUN CGO_ENABLED=0 go build -ldflags="-w -s" -o /blacked main.go

FROM alpine:latest
WORKDIR /app
COPY --from=builder /blacked /app/blacked
# Mount config and data volumes when running
EXPOSE 8082
ENTRYPOINT ["/app/blacked"]
CMD ["serve"]
```

Run with:

```bash
docker build -t blacked:latest .
docker run -d --name blacked -p 8082:8082 \
  -v $(pwd)/data:/app/data \
  -v $(pwd)/.env.toml:/app/.env.toml \
  blacked:latest
```

### Using Systemd (Linux)

Create `/etc/systemd/system/blacked.service`:

```ini
[Unit]
Description=Blacked Blacklist Service
After=network.target

[Service]
Type=simple
User=blacked
WorkingDirectory=/opt/blacked
ExecStart=/opt/blacked/blacked serve
Restart=on-failure
StandardOutput=journal

[Install]
WantedBy=multi-user.target
```

Enable and start:

```bash
sudo systemctl enable blacked.service
sudo systemctl start blacked.service
```

## 🤝 Contributing

Contributions are welcome! Please feel free to submit pull requests or open issues.

1. Fork the repository
2. Create a feature branch: `git checkout -b feature/amazing-feature`
3. Commit your changes: `git commit -m 'Add amazing feature'`
4. Push to the branch: `git push origin feature/amazing-feature`
5. Open a pull request

## 📄 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

---

<div align="center">
  <sub>Built with ❤️ for better cybersecurity</sub>
</div>
