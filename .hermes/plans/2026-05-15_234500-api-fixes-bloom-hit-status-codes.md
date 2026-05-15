# Plan: Blacked API Fixes — Status Codes, Bloom Hit, Legacy Cleanup, Bulk Endpoints

> Tarih: 2026-05-15 | Durum: Plan

## 1. Hedef

5 madde:

1. **HTTP status kodları** — 400 hatalarını content negotiation standartlarına uygun 200'li dönüşlerle değiştir (204 No Content, 200 OK with body)
2. **Eski legacy API tamamen sil** — `/entry/*` endpoint'leri kaldır
3. **Bloom filter / hit check çalışmıyor** — DB'de entry var ama bloom'dan hit gelmiyor
4. **Bulk bloom check + bulk hit endpoint'leri** — `/api/v1/bulk-check`, `/api/v1/bulk-hit`
5. **Insomnia export güncelle**

---

## 2. Root Cause: Bloom Hit Çalışmıyor

### Sorun

DB'de `airdropkiloex.live` domain var. Ama `/api/v1/check` ve `/api/v1/hit`:

```json
{"likely": false, "max_depth": 0}
{"blocked": false, "confidence": 0, "level": "informational", "matches": []}
```

### Sebep: İki Ayrı BloomManager Instance'ı

`NewQueryHandler()` her seferinde **yepyeni, boş bir BloomManager(1000)** oluşturuyor:

```go
func NewQueryHandler() (*QueryHandler, error) {
    mgr := bloom.NewBloomManager(1000)   // BOŞ — provider sync doldurmadı
    checker := NewBloomAdapter(mgr)
    // ...
}
```

Provider sync ise **başka bir BloomManager** üzerinde populate yapıyor. HTTP handler boş bloom'da sorguluyor → her zaman miss.

### Çözüm: Singleton BloomManager

Provider sync ve HTTP handler aynı BloomManager instance'ını paylaşmalı. Application seviyesinde singleton.

---

## 3. Adım Adım Plan

### Step 1: BloomManager Singleton

**Değişecek:** `features/web/handlers/v2/query_handler.go`

```go
// BloomManager, ProviderProcessService veya Application tarafından inject edilmeli
func NewQueryHandler(mgr *bloom.BloomManager) (*QueryHandler, error) {
    checker := NewBloomAdapter(mgr)
    // ...
}
```

**Değişecek:** `features/web/routes.go`

```go
// Application seviyesinde BloomManager oluştur
app.bloomManager = bloom.NewBloomManager(10000)

// Provider sync'e aynı instance'ı ver
app.ProviderProcessService = provider_processor.NewProviderProcessService(app.bloomManager)

// V2 handler'a aynı instance'ı ver
v2Handler, _ := v2.NewQueryHandler(app.bloomManager)
```

### Step 2: ProviderProcessService BloomManager Desteği

**Değişecek:** `features/providers/services/provider_process_service.go`

ProviderProcessService BloomManager almalı ve `RebuildSource`'u çağırmalı.

**Dosyayı bul ve kontrol et.**

### Step 3: HTTP Status Kodları

**Değişecek:** `features/web/handlers/v2/query_handler.go`

Mevcut:
```go
return response.BadRequest(c, "url parameter is required")  // 400
```

Olması gereken:
- URL parametre yok → `c.NoContent(http.StatusNoContent)` (204)
- Bloom check miss → `c.NoContent(http.StatusNoContent)` (204)
- Bloom check hit → `c.JSON(http.StatusOK, result)` (200)
- Hit full check miss → `c.NoContent(http.StatusNoContent)` (204)
- Hit full check hit → `c.JSON(http.StatusOK, result)` (200)
- Bulk empty → `c.NoContent(http.StatusNoContent)` (204)

Aynı düzeltme **provider handler**, **legacy entry handler** ve **benchmark handler** için de geçerli.

**Değişecek:** `features/web/handlers/response/response_handler.go`
- `BadRequest` fonksiyonu 400 yerine 204 veya hata durumu neyse ona dönüşmeli
- Veya yeni helper: `c.NoContent(http.StatusNoContent)` kullan

### Step 4: Legacy API Sil

**Silinecek dosyalar:**
- `features/web/handlers/query/` — tümü (`search_handler.go`, `search_payload.go`, `query_url.go`, `query_id.go`, `routes.go`, `bloom_checker.go`)
- `features/web/handlers/benchmark/` — tümü (isteğe bağlı)
- `features/web/handlers/provider/provider_handler.go` — sadece `/provider/processes`, `/provider/process`, `/provider/process/status/:id` kalacak

**Güncellenecek:** `features/web/routes.go`
- `query.MapQueryRoutes`, `benchmark.MapBenchmarkRoutes` satırlarını kaldır

### Step 5: Bulk Endpoint'ler

**Değişecek:** `features/web/handlers/v2/query_handler.go`

Mevcut bulk:
```go
POST /api/v1/bulk → h.svc.Bulk  // şu an her URL için Hit çağırıyor
```

Yeni bulk endpoint'ler:
- `POST /api/v1/bulk-check` → bloom only, rapid
- `POST /api/v1/bulk-hit` → bloom + DB + score

Mevcut `/api/v1/bulk`'ü `/api/v1/bulk-hit` olarak rename et. Yeni `/api/v1/bulk-check` ekle.

**Değişecek:** `features/web/handlers/v2/routes.go`
```go
g.POST("/bulk-check", handler.BulkCheck)
g.POST("/bulk-hit", handler.BulkHit)
// eski /bulk route'u kaldır (artık bulk-hit)
```

**Service katmanı:** `internal/query/service.go`
```go
func (qs *QueryService) BulkCheck(ctx context.Context, urls []string) ([]LikelyResponse, error) {
    // bloom only, hızlı
}
func (qs *QueryService) BulkHit(ctx context.Context, urls []string) ([]QueryResponse, error) {
    // bloom + DB + score
}
```

### Step 6: Insomnia Export Güncelle

**Değişecek:** `docs/insomnia-export.json`
- Status code değişikliklerine göre response description'ları güncelle
- Provider endpoint'lerinin URL'lerini düzelt (şu an doğru mu kontrol et)
- Bulk endpoint'leri ikiye ayır

---

## 4. Değişecek Dosyalar (Tam Liste)

| Dosya | İşlem | Öncelik |
|:------|:------|:--------|
| `features/web/handlers/v2/query_handler.go` | **Düzenle** — singleton BloomManager, status kodları, bulk endpoint'ler | 🔴 |
| `features/web/routes.go` | **Düzenle** — singleton inject, legacy route'ları kaldır | 🔴 |
| `features/web/handlers/v2/routes.go` | **Düzenle** — /bulk-check, /bulk-hit route'ları | 🔴 |
| `internal/query/service.go` | **Düzenle** — BulkCheck metodu ekle | 🔴 |
| `features/providers/services/provider_process_service.go` | **Kontrol/Düzenle** — BloomManager desteği | 🔴 |
| `features/web/handlers/response/response_handler.go` | **Düzenle** — 400→204 helper'ları | 🟡 |
| `features/web/handlers/query/` (tümü) | **Sil** — legacy API | 🟡 |
| `docs/insomnia-export.json` | **Düzenle** — güncel endpoint'ler | 🟢 |
| `features/web/handlers/benchmark/` | **Kaldır** (opsiyonel) | 🟢 |

---

## 5. Riskler

1. **BloomManager singleton'ı thread-safe mi?** — `BloomManager.Likely()` zaten `mu.RLock()` kullanıyor. `RebuildSource` sırasında `mu.Lock()` alıyor. Sorun yok.

2. **Provider process service hali hazırda BloomManager kullanıyor mu?** — Kontrol edilmedi. Kullanmıyorsa ProviderProcessService'e inject edilmesi gerek. Plan dışarıda kalabilir → uygulama sırasında bakılacak.

3. **Legacy API'yi silince başka bir yerde import var mı?** — `search_files` ile kontrol edilecek. Eğer `internal/query` veya başka bir paket legacy handler'ları import ediyorsa derleme hatası alırız. Safe delete: önce import'ları bul, onları da temizle.

4. **Provider handler'ın `/provider/processes` endpoint'i hâlâ çalışır durumda** — legacy silme onu etkilememeli.

---

## 6. Test / Validation

```bash
# Derleme
go build ./...

# Bloom testleri
go test ./features/bloom/ -v -count=1

# Integration
go test ./features/tests/ -v -count=1

# Manual: server çalıştır, insomnia'dan test et
```

## 7. Açık Sorular

1. **Benchmark API'si de silinsin mi?** — `POST /benchmark/query` ve `POST /benchmark/compare`. memory-freak profili için lazım olabilir.
2. **Health endpoint kalıcı mı?** — `GET /health` ve `GET /`.
3. `Provider handler` `/api/v1` prefix'i altına taşınsın mı, yoksa `/provider/processes` olarak kalsın mı?
