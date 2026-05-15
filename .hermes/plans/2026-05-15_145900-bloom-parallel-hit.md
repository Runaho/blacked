# Plan: Parallel Bloom Hit Sistemi

> Tarih: 2026-05-15 | Iteration Limit: 300 | Durum: Onay Bekliyor

## 1. Hedef

Bloom mekanizmasını yeniden tasarlamak — kaynağın verdiği kara liste girişinin seviyesine göre doğru bloom tipine yazan ve sorgu sırasında tüm bloom'lara paralelde bakıp **ilk hit'te duran** bir sistem.

---

## 2. Alınan Kararlar

Bu kararlar WHATWG URL Standard okuması ve seninle yapılan tartışma sonucu netleşti:

### Karar 1: File Detection

Sadece `path.Ext(path.Base(path)) != ""` olan son path segment'leri file sayılır. Uzantısızlar path/dizin kabul edilir.

| URL | path.Base | path.Ext | File? |
|:----|:---------:|:--------:|:-----:|
| `.../virus.exe` | `virus.exe` | `.exe` | ✅ evet |
| `.../shell.php?ref=evil` | `shell.php` | `.php` | ✅ evet |
| `.../login` | `login` | `""` | ❌ hayır → HostPath |
| `.../exploit` | `exploit` | `""` | ❌ hayır → HostPath |
| `github.com/guneskorkmaz` | `guneskorkmaz` | `""` | ❌ hayır → HostPath |

**Neden:** WHATWG URL Standard şöyle der: *"A URL path segment is an ASCII string. It commonly refers to a directory or a file, but has **no predefined meaning**."* Yani standard bu ayrımı yapmaz, karar bize ait. `path.Ext()` en güvenilir yöntem.

### Karar 2: Kaynak Ne Verdiyse O — Sağlayıcının Eksikliğini Kapatma

Bir entry **sadece tek bir bloom tipine** yazılır. Sağlayıcı query verdiyse FullURL, vermediyse File bloom'unda yaşar.

| Kaynak Girişi | Bloom | Key |
|:--------------|:------|:----|
| `.../virus.exe` | **BloomFile** | `virus.exe` |
| `.../shell.php?ref=evil` | **BloomFullURL** | `.../shell.php?ref=evil` |
| `.../exploit` (path, file değil) | **BloomHostPath** | `.../exploit` |
| `cdn.x.com` | **BloomHost** | `cdn.x.com` |
| `x.com` | **BloomDomain** | `x.com` |
| `1.2.3.4` | **BloomIP** | `1.2.3.4` |

**Altın kural:** *"Sağlayıcının eksikliğini kapatmakla uğraşma — başkasının sorunu."* Query'li URL'de File bloom'una yazma, FullURL'de yaşasın. Sağlayıcı query vermediyse File bloom'una yaz, sorguda query olsa bile File bloom'dan hit alsın.

### Karar 3: Query Kuralı

| Populate | Key | Sorgu | Sonuç |
|:---------|:----|:------|:------|
| `shell.php?ref=evil` | FullURL: `host/path/shell.php?ref=evil` | `shell.php?ref=evil` | **HIT** |
| | | `shell.php?ref=other` | **MISS** — farklı query |
| | | `shell.php` (query'siz) | **MISS** — File bloom'u boş, FullURL'de query yok |
| `shell.php` | File: `shell.php` | `shell.php?anything` | **HIT** — File bloom'dan direkt hit |
| | | `shell.php` | **HIT** |

### Karar 4: Check Sıralaması

```
Domain → Host → HostPath → File → FullURL
(En geniş → en spesifik, ilk hit'te dur)
```

Her adımda `bs.Test(key)` → true ise source-level match'leri topla, dön.

### Karar 5: Query'li URL → File Bloom'a Yazma

❌ **Yazılmaz.** Sağlayıcı `shell.php?ref=evil` verdi → sadece FullURL bloom. Query'siz sorgu `shell.php` miss alır — sağlayıcının sorumluluğu.

---

## 3. Mimari

### 3.1 Populate (Yazma)

```
Kaynak girişi → ParseURL → determineBloomTarget() → tek bloom tipine yaz
```

`determineBloomTarget(keys)` karar ağacı:

```go
func determineBloomTarget(keys *URLKeys) (BloomType, string) {
    // 1. File + Query → FullURL (en spesifik)
    if keys.File != "" && path.Ext(keys.File) != "" && keys.Query != "" && keys.Host != "" && keys.Path != "" {
        return BloomFullURL, keys.Host + keys.Path + "?" + keys.Query
    }
    // 2. File → File bloom (query dahil populate? hayır — sağlayıcı query vermediyse sadece file)
    if keys.File != "" && path.Ext(keys.File) != "" && keys.Host != "" && keys.Path != "" {
        // Query yoksa file bloom'una yaz — sorguda query olsa bile File bloom'dan hit alır
        // (sağlayıcı query vermediği için sorumluluk bizde değil? Hayır, sağlayıcı query vermemiş, o zaman file'ın her sorgusu bloklu.)
        // Ama wait — path.Ext(keys.File) kontrolü zaten yapıldı.
        return BloomFile, keys.File
    }
    // 3. File yok, Query yok → HostPath
    if keys.HostPath != "" {
        return BloomHostPath, keys.HostPath
    }
    // 4. Host only
    if keys.Host != "" {
        return BloomHost, keys.Host
    }
    // 5. Domain only
    if keys.Domain != "" {
        return BloomDomain, keys.Domain
    }
    // 6. IP
    if keys.IP != "" {
        return BloomIP, keys.IP
    }
    return "", ""
}
```

### 3.2 Check (Sorgulama)

```
Sorgu URL → ParseURL → GenerateCheckKeys() → tüm bloom'lara paralel goroutine → ilk hit cancel(other goroutines) → dön
```

`GenerateCheckKeys()` zincir sırasına göre key'leri üretir:

```go
func (uk *URLKeys) GenerateCheckKeys() []CheckKey {
    var keys []CheckKey

    // 1. Domain (en geniş)
    if uk.Domain != "" {
        keys = append(keys, CheckKey{BloomDomain, uk.Domain})
    }
    // 2. Host
    if uk.Host != "" {
        keys = append(keys, CheckKey{BloomHost, uk.Host})
    }
    // 3. HostPath variants — en yüzeyden en derine (ilk hit parent'ta durur)
    if uk.Host != "" && uk.Path != "" {
        parents := parentPaths(uk.Path)        // ["/a", "/a/b", "/a/b/c"] — en yüzeyden
        for _, p := range parents {
            keys = append(keys, CheckKey{BloomHostPath, uk.Host + p})
        }
    }
    // 4. File
    if uk.File != "" {
        keys = append(keys, CheckKey{BloomFile, uk.File})
    }
    // 5. FullURL (en spesifik)
    if uk.Host != "" && uk.Path != "" {
        fullURL := uk.Host + uk.Path
        if uk.Query != "" {
            fullURL += "?" + uk.Query
        }
        keys = append(keys, CheckKey{BloomFullURL, fullURL})
    }

    return keys
}
```

> **Not:** FullURL en sonda — çünkü File bloom'u daha önce kontrol edilir (query'siz). FullURL query'li varyant için. Domain en başta — tüm subdomain'leri kapsar.

### 3.3 HostPath parentPaths

```go
// parentPaths returns all parent directories from shallowest → deepest.
// "/a/b/c/file.exe" → ["/a", "/a/b", "/a/b/c"]
func parentPaths(p string) []string {
    p = strings.TrimSuffix(p, "/")
    if p == "" || p == "/" {
        return nil
    }
    // Build all prefixes
    parts := strings.Split(p, "/")
    var result []string
    for i := 1; i < len(parts); i++ {
        result = append(result, strings.Join(parts[:i+1], "/"))
    }
    return result
}
```

En yüzeyden en derine — `/a` önce kontrol edilir, sonra `/a/b`, sonra `/a/b/c`. İlk hit'te durulacağı için parent path daha önce hit eder.

---

## 4. Değişecek Dosyalar

### `features/bloom/types.go`
- `BloomFullURL BloomType = "full_url"` sabiti
- `DepthWeight` map'ine `BloomFullURL: 1.5`

### `features/bloom/url_parser.go`
- `ParseURL`: File detection → `path.Ext(path.Base(path)) != ""`
- `parentPaths()` helper
- `GenerateCheckKeys()` metodu (check key'leri, zincir sırasına göre)
- `CheckKey` struct'ı

### `features/bloom/manager.go`
- `NewBloomManager`: `BloomFullURL`'ü sets'e ekle
- `determineBloomTarget()` — populate karar ağacı
- `PopulateEntry()` — tek bloom tipine yaz
- `Likely()` — parallel check + early return (goroutine + context cancel)

### `features/bloom/bloom_set.go`
- Değişmez (Test/TestSource zaten RLock korumalı)

### `features/bloom/bloom_test.go`
- Mevcut test beklentilerini yeni populate mantığına güncelle
- Yeni testler: `TestParentPath_Match`, `TestFullURL_NoParentMatch`, `TestDifferentSubdomain_NoMatch`, `TestQuery_ProviderResponsibility`

### `features/tests/blacked_integration_test.go`
- `populateBloom()` → `PopulateEntry` kullan
- B grubu test beklentilerini güncelle
- D_2/D_3 beklentilerini güncelle
- F_9 artık `Likely=false` ✅

### `features/entry_collector/pond_collector.go`
- Bloom populate çağrısı varsa `PopulateEntry` kullanacak şekilde güncelle

### `features/e2e/blacked_e2e_test.go`
- E2E'de bloom manager kullanımı varsa kontrol et

---

## 5. Test Datası — Onay Bekleyen

Her bloom tipi için **izole host'lar** kullanılıyor. Test datası `bloom_test_data.json` şu anda draft aşamasında — son kararlara göre güncellenecek.

### Populate

| Bloom Tipi | Host | Girişler |
|:-----------|:-----|:---------|
| **FullURL** | `cdn.fullurl-bloom-test.com` | `.../malware/virus.exe`, `.../exploit/shell.php?ref=evil` |
| **HostPath** | `www.hostpath-bloom-test.com` | `/exploit`, `/malware` |
| **Host** | `sub.host-bloom-test.com` | sadece host |
| **Domain** | `domain-bloom-test.com` | sadece domain |
| **IP** | `10.20.30.40`, `10.20.30.41` | bare IP'ler |

**Dikkat:** File bloom testi için ayrı bir host kullanılabilir (örneğin `files.bloom-test.com`):

| Bloom Tipi | Host | Girişler |
|:-----------|:-----|:---------|
| **File** | `files.bloom-test.com` | `porn.jpg`, `video.mp4` (query'siz, sadece file) |

### Check — Beklenen Sonuçlar

**FullURL bloom:**
| Sorgu | Sonuç | Açıklama |
|:------|:-----:|:---------|
| `cdn.fullurl-bloom-test.com/malware/virus.exe` | **HIT** | File var → FullURL |
| `cdn.fullurl-bloom-test.com/malware/other.dll` | **MISS** | Farklı file, FullURL bloom farklı key |
| `cdn.fullurl-bloom-test.com/exploit/shell.php?ref=evil` | **HIT** | Query dahil exact match |
| `cdn.fullurl-bloom-test.com/exploit/shell.php?ref=other` | **MISS** | Farklı query, sağlayıcının sorumluluğu |
| `cdn.fullurl-bloom-test.com/exploit/shell.php` | **MISS** | Query'siz sorgu, query'li populate |

**HostPath bloom:**
| Sorgu | Sonuç | Açıklama |
|:------|:-----:|:---------|
| `www.hostpath-bloom-test.com/exploit` | **HIT** | Exact path match |
| `www.hostpath-bloom-test.com/exploit/shell.php` | **HIT** | Parent path match (path altındaki her şey) |
| `www.hostpath-bloom-test.com/exploit/deep/path/file.exe` | **HIT** | Derin parent path |
| `www.hostpath-bloom-test.com/other` | **MISS** | Farklı path |
| `api.hostpath-bloom-test.com/exploit` | **MISS** | Farklı host altında aynı path |

**Host bloom:**
| Sorgu | Sonuç | Açıklama |
|:------|:-----:|:---------|
| `sub.host-bloom-test.com/anything` | **HIT** | Subdomain altındaki her şey |
| `sub.host-bloom-test.com/` | **HIT** | Subdomain'in kendisi |
| `other.host-bloom-test.com` | **MISS** | Farklı subdomain |

**Domain bloom:**
| Sorgu | Sonuç | Açıklama |
|:------|:-----:|:---------|
| `domain-bloom-test.com/any/path` | **HIT** | Domain altındaki her şey |
| `sub.domain-bloom-test.com/other` | **HIT** | Farklı subdomain bile hit |
| `other-domain.test` | **MISS** | Farklı domain |

**IP bloom:**
| Sorgu | Sonuç | Açıklama |
|:------|:-----:|:---------|
| `10.20.30.40/bot.exe` | **HIT** | IP bloom'dan (path içermez) |
| `10.20.30.40/other.exe` | **HIT** | Aynı IP, farklı path |
| `10.20.30.41/something` | **HIT** | IP bloom'dan |
| `10.20.30.99` | **MISS** | Farklı IP |

**File bloom:**
| Sorgu | Sonuç | Açıklama |
|:------|:-----:|:---------|
| `files.bloom-test.com/path/porn.jpg` | **HIT** | File bloom'dan `porn.jpg` |
| `files.bloom-test.com/other/video.mp4` | **HIT** | File bloom'dan `video.mp4` |
| `files.bloom-test.com/else/other.jpg` | **MISS** | Farklı file |

**Zincir testi (domain bloklu, altı önemsiz):**
| Sorgu | Sonuç | Açıklama |
|:------|:-----:|:---------|
| `sub.domain-bloom-test.com/images/exploit.php` | **DOMAIN HIT** | Domain en üstte, Host/HostPath/File'a bakmaz bile |

---

## 6. Uygulama Adımları

### Step 1: `types.go` — BloomFullURL
- `BloomFullURL` sabiti
- `DepthWeight` güncelle

### Step 2: `url_parser.go` — ParseURL + Check Keys
- File detection düzeltmesi (`path.Ext`)
- `parentPaths()` helper
- `CheckKey` struct
- `GenerateCheckKeys()` metodu

### Step 3: `manager.go` — Populate + Parallel Likely
- `NewBloomManager` güncelle
- `determineBloomTarget()`
- `PopulateEntry()`
- `Likely()` parallel rewrite

### Step 4: `bloom_test.go` — Unit test güncelle
- Mevcut test beklentilerini güncelle
- Yeni testler ekle

### Step 5: `blacked_integration_test.go` — Integration test güncelle
- `populateBloom()` → `PopulateEntry`
- B/D/F test beklentileri

### Step 6: `pond_collector.go` — Diğer kullanımlar
- Bloom populate çağrılarını güncelle

### Step 7: Çalıştır
```bash
go test ./features/bloom/ -v -count=1
go test ./features/tests/ -v -count=1
go test ./features/e2e/ -v -count=1 -tags=e2e
```

### Step 8: Commit + Task 2 kapat + Task 3 builder

---

## 7. Riskler

1. **File detection değişikliği** — ParseURL'de File alanı artık sadece uzantılıları doldurur. DB'deki mevcut `file` kolonunda uzantısız veriler kalabilir. Populate DB'den `source_url`'yi okuyup ParseURL'den geçirdiği için sorun yok — eski DB verileri de yeni ParseURL ile geçer.

2. **Paralel goroutine sayısı** — Sorgu başına ~5-8 goroutine. Bloom Test O(1) → toplam < 0.5ms.

3. **Concurrent safety** — `BloomSet.Test()` ve `TestSource()` zaten `RLock` korumalı. Deadlock yok.

4. **Context cancel** — `resultCh` buffered(1), tek goroutine kanala yazar, diğerleri `ctx.Done()` ile erken döner. `wg.Wait()` temiz kapanma garantisi.
