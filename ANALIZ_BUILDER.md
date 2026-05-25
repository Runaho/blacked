# Analiz Raporu — Builder

## 1. Sistem Mimarisi

### High-Level Component'ler ve İlişkiler

Sistem 4 katmandan oluşuyor:

```
Provider Katmanı (Fetch → Parse → Submit)
         ↓
PondCollector (Buffer → Single-threaded DB writer → BloomManager populate)
         ↓
BloomManager (in-memory bloom filters, singleton)
         ↓
QueryService (HTTP-agnostic core: bloom → DB confirm → score)
         ↓
Echo Handlers (v2 API: /check, /hit, /bulk-check, /bulk-hit)
```

**Mevcut component'ler:**
- `features/providers/` — 16+ provider (URLhaus, OISD, OpenPhish, PhishTank, vb.)
- `features/entry_collector/` — `PondCollector` singleton, buffer + single-threaded DB writer
- `features/bloom/` — `BloomManager` + 7 active BloomType (Domain, Host, HostPath, File, FullURL, Login, IP)
- `internal/query/` — `QueryService`, `Scorer`, adapter pattern for bloom
- `features/web/handlers/v2/` — Echo handlers wiring bloom to HTTP
- `internal/runner/` — `gocron` scheduler, startup decision engine
- `internal/db/` — SQLite read/write pool

**Dependency injection:** Interface-based. `BloomChecker`, `ScorerIface`, `BlacklistRepository` soyutlamaları iyi. Ancak `PondCollector` global singleton olarak erişiliyor — test/mock zorluğu var.

---

**Bulgu:** BloomManager singleton'u PondCollector içinde yaratılıyor ama `entry_collector.GetPondCollector()` ile global erişim var. QueryService ise bu BloomManager'ı handler'da oluşturuyor. İki farklı bloom instance'ı olabilir riski.
**Etki:** High
**Öneri:** BloomManager tek bir yerde yaratılmalı ve interface üzerinden inject edilmeli. Şu an `NewQueryHandler` → `NewBloomAdapter(mgr)` yapıyor, mgr'yi `collector.GetBloomManager()`'dan alıyor. Bu doğru ama init sırası kritik — eğer `serve` command'i http server'ı başlatırken `entry_collector.InitPondCollector` daha önce çağrılmamışsa, `GetBloomManager()` nil döner.

---

**Bulgu:** Config system — `config/scoring.toml` ayrı, `config/` directory'si ise config dosyaları değil (grafana/prometheus/tempo). Config tek bir `Config` struct'ında koanf ile yükleniyor. ProviderOptions'ta `Extra` map var ama kullanımı tutarsız.
**Etki:** Medium
**Öneri:** Config'i provider bazında ayırmak için ayrı provider config dosyaları veya env var desteği eklenebilir.

---

## 2. İş Akışları (Workflows)

### Provider → Collector → Database akışı

```
Provider.Fetch() → Colly/http → ResponseReader
       ↓
Provider.Parse() → ParseLinesParallel() → Entry[] → PondCollector.Submit()
       ↓
Buffer (batch_size=1000, PeriodicFlushInterval=5s) → dbWriteChan (buffered 100)
       ↓
singleThreadedDBWriter() → repo.BatchSaveEntries() → BloomManager.PopulateEntry()
```

**Olumlu:** Tek threaded DB writer ile SQLite WAL mode'un doğru kullanımı — lock contention yok. Batch buffer ve sync.Pool ile memory yönetimi iyi.

**Sorun:** `pond_collector_cache_sync.go` — cache sync state machine'i var ama cache sync tam olarak ne yapıyor anlamak zor. `ScheduleCacheSync(true)` immediate sync tetikliyor ama Badger cache'in ne tuttuğu ve bloom ile nasıl etkileştiği net değil. `cache.InitializeCache` ve `cache.CloseCache` var ama cache'in amacı belgelenmemiş.

---

**Bulgu:** Provider processing — `process.go` `Providers.Process()` semaphore ile concurrency control yapıyor. Max concurrent = `len(p)` (tüm provider'lar paralel). Ancak her provider'ın fetch süresi çok farklı (OISD ~2M domain, URLhaus ~50K). Uzun süren provider diğerlerini block etmez çünkü semaphore her provider için ayrı goroutine'de çalışıyor.
**Etki:** Low
**Öneri:** Per-provider timeout ve retry logic eksik. Bir provider'ın takılması durumunda diğerleri çalışmaya devam eder — bu iyi.

---

### Web API → Bloom → Response akışı

```
HTTP GET /api/v1/check?url= → QueryHandler.Check()
       ↓
QueryService.Likely() → BloomAdapter.Check() → BloomManager.Likely()
       ↓
result.Likely ? 200 JSON : 204 NoContent
```

```
HTTP GET /api/v1/hit?url= → QueryHandler.Hit()
       ↓
QueryService.Hit() → bloom.Check() → DB.ExistsByHost/Domain/IP/BloomType → Scorer.Score()
       ↓
result.Blocked ? 200 JSON : 204 NoContent
```

**Olumlu:** HTTP-agnostic QueryService — test edilebilir, başka transport eklenebilir. Adapter pattern bloom'u interface üzerinden bağlıyor.

**Sorun:** `BulkCheck` ve `BulkHit` serial döngü ile çalışıyor. Her URL ayrı `Hit()` çağrısı yapıyor. 100 URL varsa 100 × ~5-15ms = 500ms-1.5s. Parallelize edilebilir.
**Etki:** Medium
**Öneri:** `BulkHit` için goroutine-based parallel processing, semaphore ile concurrency limit.

---

### Background Job / Cron pattern'leri

`gocron` kullanılıyor. `runner.go` — `NewRunner()`, `RegisterProvider()`, `Start()`, `Stop()`. Singleton mode `LimitModeReschedule` — önceki iş bitmeden yeni iş başlarsa reschedule ediliyor.

**Sorun:** `executeProvider` — `UpdateCacheDeferred` kullanıyor, `UpdateCacheImmediate` değil. Bu cron job'ların cache'i daha geç güncellemesi anlamına geliyor. Ancak server startup'ta `RunStartupProviders` veya `RunProviderJobsAsync`Immediate cache mode kullanıyor.

**Sorun:** `serve` command'i `graceful.Graceful(server.ListenAndServe, server.Shutdown)` kullanıyor ama `ShutdownTimeout` config'te 30s var ama graceful'un context timeout'u set edilmemiş görünüyor. HTTP server graceful shutdown tam implement edilmemiş.
**Etki:** High
**Öneri:** `graceful.Graceful` yerine native `http.Server` Shutdown method'u ile graceful shutdown implement edilmeli.

---

## 3. Entegrasyon Noktaları

### Provider'lar arası tutarlılık

Provider'lar `base.NewBaseProvider` üzerinden yaratılıyor. Parse fonksiyonu `ParseLinesParallel` — tüm provider'lar için aynı pattern. Yapı tutarlı.

**Sorun:** Provider registration — `provider.Register()` çağrısı gerekiyor. Kimi provider'lar调用 yapıyor (OISD), kimi yapmıyor (URLhaus — dosyada `Register()` çağrısı görmedim, tam dosyayı görmem lazım).
**Etki:** Medium
**Öneri:** `InitProviders()` tüm provider'ları otomatik register etmeli — manual `Register()` çağrısı yerine.

---

### Web handler'ların API contract'ları

v2 routes — `/api/v1/check`, `/api/v1/hit`, `/api/v1/bulk-check`, `/api/v1/bulk-hit`. `/api/v1/` prefix'i var ama routing layer'da versionlama yok.

**Sorun:** v1 handler'lar (response handler, benchmark handler) farklı path'lerde. v2 ile v1 arasında geçiş veya deprecation planı belgelenmemiş.
**Etki:** Low
**Öneri:** API versioning stratejisi belgelenmeli.

---

### Config sistemi ile provider'ların bağlantısı

Config `koanf` ile yükleniyor. `config.InitConfig()` `before()` hook'ta çağrılıyor. Provider options map'i `cfg.Providers["provider-name"]` ile erişiliyor.

**Sorun:** Config dosyası olarak sadece `scoring.toml` var, provider config.toml veya yaml yok. Provider'lar hardcoded default değerler kullanıyor (URLhaus'ta `https://urlhaus.abuse.ch/downloads/text/` gibi). Config üzerinden override edilemiyor — sadece code'da değişiklik mümkün.
**Etki:** Medium
**Öneri:** Provider config'i ayrı bir toml dosyası veya koanf environment variable desteği eklenmeli.

---

### External API entegrasyonları

Provider'lar colly veya `HTTPFetcher` ile fetch yapıyor. URLhaus text formatı, OISD domainswild2 formatı. Parse fonksiyonları provider-specific.

**Sorun:** Bir provider'ın API'si değiştiğinde (format farklılaştı, endpoint değişti) tüm provider'ı güncellemek gerekiyor. Soyutlama yok — her provider kendi parse implementasyonu yapıyor. Ortak bir `EntryParser` interface'i olabilir.
**Etki:** Medium
**Öneri:** `EntryParser` interface'i tanımlanabilir — line parser, JSON parser, CSV parser gibi implementations.

---

## 4. Operasyonel İyileştirmeler

### Logging/monitoring eksiklikleri

Zerolog ile güzel structured logging var. `log.Info()`, `log.Debug()`, `log.Trace()` seviyeleri doğru kullanılmış.

**Sorun:** Business-level metrics yok — örneğin "son 24 saatte kaç URL hit edildi", "en çok hit edilen domain'ler", "cache hit rate". Sadece Prometheus metrics var (sync duration, entries processed, vb.) ama business observability eksik.
**Etki:** Medium
**Öneri:** Custom Prometheus metrics — `blacked_url_check_total`, `blacked_hit_rate`, `blacked_false_positive_rate` gibi.

---

### Error reporting/alerting fırsatları

Error'lar loglanıyor ama alerting yok. Örneğin provider fetch hatalarında slack/email notification yok. `process.go` `aggregatedError` döndürüyor ama caller bunu handle etmiyor (main.go'da `before()` hook'ta provider init hatası var, command'lerde yok).

**Sorun:** `providers.InitProviders()` hata döndürüyor ama `serve` command'inde kontrol edilmiyor — sadece `log.Error` var. Server start olmadan devam edebilir.
**Etki:** High
**Öneri:** Provider init failure'lerde server'ın başlamaması veya en azından warning vermesi.

---

### Testing strategy

Provider testleri, integration testleri var (`provider_integration_test.go`). `pond_collector_test.go` var. Unit testler için mock'lar eksik — özellikle `QueryService` testleri için `BloomChecker` ve `EntryRepository` mock'ları gerekiyor.

**Sorun:** Coverage bilinmiyor. `go test ./... -cover` çalıştırılmamış.
**Etki:** Medium
**Öneri:** CI'da coverage enforce edilmeli.

---

### Deployment/DevOps concerns

Dockerfile var, docker-compose.yml var (postgres placeholder yok, sadece prometheus/grafana/tempo). Tek başına çalışıyor.

**Sorun:** pprof endpoint'leri `/debug/pprof/*` — production'da expose edilmemeli. Auth yok.
**Etki:** High
**Öneri:** pprof'ü sadece dev/staging'te enable eden config flag'i olmalı.

---

**Bulgu:** `CacheSettings` config'te `UseBloom` ve `CacheType` var ama cache'in ne için kullanıldığı net değil. `cache.InitializeCache()` `badger` kullanıyor. Ancak `BloomManager` in-memory bloom filter'lar kullanıyor — Badger cache ne tutuyor? Entry'lerin cached hali mi? Response'lar mı?
**Etki:** Medium
**Öneri:** Cache layer'ın sorumluluğu netleştirilmeli — docs veya code comment eklenmeli.

---

**Bulgu:** `BloomSet.ResetSource()` O(N) — tüm source ID'leri iterate edip delete yapıyor. 1000 source varsa ve bir source reset edilirse 999 iterate edilir. Source sayısı arttıkça bu pahalı.
**Etki:** Medium
**Öneri:** `map[string]map[string]bool` yerine sourceID → key map'i ayrı tutulabilir veya reset O(1) yapılabilir.

---

## 5. Öncelikli İyileştirmeler (Top 10)

| # | Bulgu | Etki | Öneri |
|---|-------|------|-------|
| 1 | Graceful HTTP shutdown eksik — graceful kullanılıyor ama context timeout yok | High | `graceful.Graceful` yerine `http.Server.Shutdown(ctx)` implementasyonu |
| 2 | pprof endpoint'leri auth yok — production'da exposure riski | High | pprof için config flag (`server.pprof_enabled`) |
| 3 | Provider init hatası server'ı başlatmıyor ama handle edilmiyor | High | `serve` command'inde `providers.InitProviders()` return değeri kontrol edilmeli |
| 4 | `BulkHit` serial — 100 URL × ~5ms = 500ms | Medium | Goroutine-based parallel bulk processing |
| 5 | Business metrics yok — URL check/hit rate, false positive rate | Medium | Custom Prometheus metrics eklenmeli |
| 6 | Cache layer'ın sorumluluğu net değil — Badger ne tutuyor? | Medium | Code comment veya docs ile açıklanmalı |
| 7 | BloomSet.ResetSource() O(N) — source count arttıkça pahalı | Medium | Data structure değişikliği — sourceID → key map ayrılabilir |
| 8 | Provider registration manual — `Register()` çağrısı gerekiyor | Medium | Auto-registration via init() or package-level scan |
| 9 | API versioning stratejisi yok — v1/v2 geçiş belirsiz | Low | API versioning doc veya plan |
| 10 | Coverage enforce edilmiyor — test quality bilinmiyor | Low | CI'da `go test -cover` threshold enforcement |

---

## Özet

**Sistem mimarisi kalitesi: 7/10**

**Güçlü yanlar:**
- Clean separation of concerns — provider/collector/bloom/query/http katmanları
- Interface-based design — `BloomChecker`, `ScorerIface`, `BlacklistRepository` soyutlamaları iyi
- Single-threaded DB writer ile SQLite lock contention çözümü
- Structured logging (zerolog) and OpenTelemetry tracing
- Adapter pattern ile HTTP-agnostic query core

**Ana iyileştirme fırsatları:**
1. Graceful HTTP shutdown implementasyonu — en kritik operasyonel eksik
2. pprof auth — production security risk
3. Bulk operations parallelization — performans
4. Business observability — custom metrics
5. Cache layer'ın netleştirilmesi — technical debt

Sistem karmaşık değil, anlaşılır ve bakımı yapılabilir. Technical debt öncelikli olarak yukarıdaki 10 item ile azaltılabilir.