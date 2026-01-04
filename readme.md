<img src="https://github.com/user-attachments/assets/a54c8d22-77ba-4a05-9d86-aca811d1c1f9" alt="Blacked Logo" height="150px" />

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
ttl = "10m"                 # Time to live for cache entries default is 5m if not set everything is cached to forever

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
go run . serve

# Or with the built binary
./blacked serve
```

The server will start on `http://localhost:8082` by default (configurable in `.env.toml`).

### CLI Commands

Blacked includes a robust CLI for direct interaction:

```bash
# Process all providers immediately
go run . process

# Process specific providers only
go run . process --provider OISD_BIG --provider URLHAUS

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

| Endpoint              | Method | Description                            | Example                                            |
| --------------------- | ------ | -------------------------------------- | -------------------------------------------------- |
| `/entry`            | GET    | Quick check if a URL is blacklisted    | `/entry?url=example.com`                         |
| `/entry/likely`     | GET    | Bloom check return 404 or 200          | `/entry/likely?url=example.com`                  |
| `/entry/{id}`       | GET    | Get details for a specific entry by ID | `/entry/550e8400-e29b-41d4-a716-446655440000`    |
| `/entry/search`     | POST   | Advanced search with query options     | `{"url": "example.com", "query_type": "domain"}` |
| `/provider/process` | POST   | Trigger provider processing            | `{"providers_to_process": ["URLHAUS"]}`          |
| `/benchmark/query`  | POST   | Benchmark query performance            | `{"urls": ["example.com"], "iterations": 100}`   |

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

func NewMyProvider(settings *config.CollectorConfig, collyClient *colly.Collector) base.Provider {
    const (
        providerName = "MY_PROVIDER"
        providerURL  = "https://example.com/blacklist.txt"
        cronSchedule = "0 */6 * * *" // Every 6 hours
    )

    // Define how to parse provider data
    parseFunc := func(data io.Reader, collector entry_collector.Collector) error {
        // Parse the data format specific to this provider
        // Submit entries.Entry to collector
        //
        collector.Submit(*entry)
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

func getProviders(cfg *config.Config, cc *colly.Collector) Providers {
	oisd.NewOISDBigProvider(&cfg.Collector, cc)

	/* ... */
		// Add your new provider here
	/* ... */

	providers := Providers(base.GetRegisteredProviders())
	return providers
}

```

## 📦 Deployment

### Using Docker

```bash
docker build -t blacked:latest .
docker run -d --name blacked -p 8082:8082 \
  -v $(pwd)/data:/app/data \
  -v $(pwd)/.env.toml:/app/.env.toml \
  blacked:latest
```

### Using Docker Compose

You can use docker compose for example deployments It's implemented with opentelemetry and prometheus metrics out of the box.

#### Podman Commands

```bash
podman compose -f f:\Projects\blacked\docker-compose.yml down
podman compose -f f:\Projects\blacked\docker-compose.yml up -d --build
```

#### Docker Compose Commands

```bash
docker-compose -f ./docker-compose.yml down
docker-compose -f ./docker-compose.yml up -d --build
```

#### Accessing Traces with go tool trace

First open it from environment;

*BLACKEDEXECTRACE=1*

*BLACKEDEXECTRACE_SCOPE=providers*

Then run the server or process command to generate trace files.
Wait the message that trace file is saved '**Go exec trace stopped**', then run:

```bash
podman cp blacked:/app/traces/ f:\Projects\blacked\traces\                                                                              
go tool trace .\traces\traces\providers-x.out
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
