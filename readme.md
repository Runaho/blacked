# Blacked

Blacked is a blacklist aggregation and query service written in Go. It fetches, parses, and stores blacklist entries from various providers, allowing users to query entries by URL. It provides both a command-line interface (CLI) and a web API for interaction.

Goal is to be fast and easy to deploy like one executable and embeded db. Embeded software is a goal here so CLI & API first development approach would be appreciated.

## Features

*   **Blacklist Aggregation:**  Fetches and processes blacklist entries from multiple providers like OISD, OpenPhish, and URLhaus.
*   **Data Persistence:**  Uses SQLite as the database to store blacklist entries.
*   **URL Querying:**  Provides the ability to query blacklist entries based on full URL, host, domain, or path.
*   **Command-Line Interface (CLI):** Offers a CLI for processing providers and querying blacklist entries.
*   **Web API:**  Exposes a web API for programmatic interaction, including processing providers and querying entries.
*   **Concurrency:** Processes providers concurrently for faster data aggregation.
*   **Metrics:** Exposes Prometheus metrics for monitoring the service's performance.
*   **Configuration:**  Configurable via TOML and environment variables.
*   **Soft Deletes:** Implements soft deletes for blacklist entries, allowing for easy removal and potential restoration.
*   **Asynchronous Processing:** Initiates background processing of providers via the CLI.
*   **Process Management:**  Tracks provider processing tasks and exposes status via API.

## Architecture

The project follows a modular architecture, with components separated into packages:

*   **`cmd`:** Contains the CLI and entrypoint logic.
*   **`features`:**  Houses the core functionalities, separated into:
    *   **`entries`:** Defines the data structure for blacklist entries and query logic.
    *   **`providers`:** Handles provider fetching, parsing, and processing.
    *   **`web`:**  Implements the web API using Echo framework.
*   **`internal`:**  Contains internal utilities and shared components like database connection management, configuration loading, and logging.

## Getting Started

### Prerequisites

*   Go 1.24 or higher
*   SQLite

### Installation

1.  **Clone the repository:**

    ```bash
    git clone https://github.com/Runaho/blacked
    cd blacked
    ```

2.  **Build the application:**

    ```bash
    go build -o blacked ./cmd/main.go
    ```

### Configuration

The application uses a configuration file (default `.env.toml`) and environment variables. Create a `config/.env.toml` file based on the example below or use environment variables:

```toml
[APP]
environtment = "development"  # or "production"
log_level = "debug" # or "info", "warn", "error", "fatal", "panic"

[Server]
scheme = "http"
port = 8082
host = "localhost"
read_timeout = "5s"
write_timeout = "10s"
shutdown_timeout = "30s"
alloworigins = ["*"] # Example: ["http://localhost:3000", "https://example.com"]
health_check = true

[Cache]
cache_refresh_interval = "5m"

[Collector]
max_workers = 10
batch_size = 100
cron = "0 0 0 * * *"
store_responses = false
store_path = "./responses"

[Colly]
max_redirects = 10
max_size = 1048576
max_depth = 1
user_agent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/58.0.3029.110 Safari/537.3"
timeout = "5m"
```

### Running the Application

1.  **Run the web server:**

    ```bash
    ./blacked serve
    ```

    This will start the web API server on port 8082 (or the port specified in your configuration).

2.  **Process blacklist providers (CLI):**

    ```bash
    ./blacked process
    ```

    This will fetch and process blacklist entries from all configured providers.

    You can specify specific providers:

    ```bash
    ./blacked process -p OISD_BIG,OPENPHISH
    ```

    You can remove providers:

    ```bash
    ./blacked process -r PHISHTANK_ONLINE_VALID
    ```

    You can force a process even if another is running:

    ```bash
    ./blacked process -f
    ```

3.  **Query blacklist entries (CLI):**

    ```bash
    ./blacked query -u example.com
    ```

    This will query the blacklist for entries matching `example.com`.

    You can specify the query type:

    ```bash
    ./blacked query -u example.com -t host
    ```

    Available query types are: `full`, `host`, `domain`, `path`, `mixed`.

    Output results in JSON format:

    ```bash
    ./blacked query -u example.com -j
    ```

    Enable verbose logging to see all hits:

    ```bash
    ./blacked query -u example.com -v
    ```

### API Endpoints

*   **`GET /health/status`:** Health check endpoint.
*   **`POST /query/entry`:** Query blacklist entries.  Requires a JSON payload with `url` and `query_type` fields.
*   **`POST /provider/process`:** Start processing providers. Requires a JSON payload with `providers_to_process` and `providers_to_remove` fields.
*   **`GET /provider/process/status/:processID`:** Get the status of a provider processing task.
*   **`GET /provider/processes`:** List all provider processing tasks.
*   **`GET /metrics`:**  Prometheus metrics endpoint.
*   **`GET /metrics/prometheus`:** Prometheus metrics endpoint (in Prometheus format).

## Development

### Testing

Run tests:

```bash
go test ./...
```

### Code Style

Follow standard Go coding conventions.  Use `go fmt` to format your code.

### Implementing a New Provider

To add a new blacklist provider, follow these steps:

1.  **Create a new provider file:** Create a new file in the `features/providers` directory (e.g., `features/providers/newprovider/newprovider.go`).

2.  **Define the provider struct:** Create a struct that implements the `providers.Provider` interface: you can copy the other providers and modify them according to your needs.  Here is an example:

```go
// Unique name of the provider will be used in logs, metrics, cli and api
func (n *NewProvider) Name() string {
	return "NEW_PROVIDER"
}

// Source URL of the blacklist
func (n *NewProvider) Source() string {
		return "https://example.com/blacklist.txt"
	}

// Usially fetch method is same as other providers
func (n *NewProvider) Fetch() (io.Reader, error) {
	// technically, colly will parse the response body as a reader then processor will pass it to the Parse method
	return bytes.NewReader(responseBody), nil
}

func (n *NewProvider) Parse(data io.Reader) error {
	// before that could be same as other providers
	// Customize the parsing logic according to the provider's data format
	for scanner.Scan() {
		scanningAt := time.Now()
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		_entry := new(entries.Entry)
		_entry.ID = uuid.New().String()
		_entry.ProcessID = processID.String()
		_entry.Source = n.Name()
		_entry.SourceURL = n.Source()
		_entry.CreatedAt = time.Now()
		_entry.UpdatedAt = time.Now()

		err := _entry.SetURL(line)

		entry := *_entry
		// after that could be same as other providers
}
    ```

    *   **`Name() string`:** Returns the unique name of the provider.
    *   **`Source() string`:** Returns the URL of the blacklist source.
    *   **`Fetch() (io.Reader, error)`:** Fetches the blacklist data from the source URL.  This should return an `io.Reader` for the data.
    *   **`Parse(data io.Reader) error`:** Parses the data from the `io.Reader` and saves the entries to the database. Use a `bufio.Scanner` to read line by line and `_entry.SetURL(line)` to initialize and validate the entry.  Batch insert records via `o.repository.BatchSaveEntries(ctx, entryBatch)` for better performance.
    *   **`SetProcessID(id uuid.UUID)`:** Sets the process ID for the current processing run.
    *   **`GetProcessID() uuid.UUID`:** Gets the process ID.
    *   **`SetRepository(repository repository.BlacklistRepository)`:** Sets the repository instance for saving entries.

3.  **Register the provider:**  Modify `features/providers/init.go` to import the new provider and register it in the `NewProviders` function:

```go
package providers

func NewProviders() (*Providers, error) {
// ----
	providers := &Providers{
		oisd.NewOISDBigProvider(&cfg.Collector, cc),
		oisd.NewOISDNSFWProvider(&cfg.Collector, cc),
		openphish.NewOpenPhishFeedProvider(&cfg.Collector, cc),
		urlhaus.NewURLHausProvider(&cfg.Collector, cc),

		newprovider.NewNewProvider(&cfg.Collector, cc), // Register your new provider
	}

// ----

	return providers, nil
}
    ```

4.  **Update `SourceDomains()`**: Add the new provider's domain in the `SourceDomains()` function in `features/providers/main.go` to allow the colly client to visit the URL.

5.  **Test your implementation:**  Create a test file (e.g., `features/providers/newprovider/newprovider_test.go`) and write tests for your new provider to ensure it fetches and parses data correctly.  See existing provider tests for examples.

6.  **Run the application and verify that the new provider is processed.**  Check the logs for any errors.

**Important Considerations:**

*   **Error Handling:** Implement robust error handling in the `Fetch` and `Parse` methods.  Use `zerolog` for logging.
*   **Data Format:**  Handle different data formats (e.g., plain text, JSON, CSV) appropriately in the `Parse` method.
*   **Rate Limiting:**  Respect the provider's rate limits to avoid being blocked.  Consider implementing your own rate limiting mechanism.
*   **Performance:**  Optimize the `Parse` method for performance, especially when dealing with large blacklist files.  Use batch inserts for database operations.
*   **Testing:** Write comprehensive unit tests to ensure your provider is working correctly.
*   **Metrics:** Increment Prometheus metrics to track the performance of your new provider.

Following these steps will allow you to easily add new providers to the Blacked blacklist aggregation service. Remember to thoroughly test your provider and consider the important considerations outlined above.

## License

This project is licensed under the [MIT License](LICENSE).

## Future Enhancements

*   Implement additional blacklist providers.
*   Add caching mechanisms for faster query performance.
*   Support additional database backends with some cloud support (e.g., Cockroachdb ).
* 	Support other response saving options like (.eg, S3 SDK)
*   Add input validation and sanitization to prevent security vulnerabilities.
*   Implement rate limiting for per provider.
*   Implement cron for provider data sync process per provider level with custom cron times and also some provider's insert changes on top of the file this could be a nice touch. 
*   Consider using a dedicated task queue for background provider processing. (maybe implement asynq?)
*   Investigate using a bloom filter to speed up the querying process or search engine?
