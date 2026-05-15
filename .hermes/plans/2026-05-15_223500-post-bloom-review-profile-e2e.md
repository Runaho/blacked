# Plan: Post-Bloom Refactor — Review, Profile, E2E

> Tarih: 2026-05-15 | Durum: Plan

## 1. Hedef

Mad-man Parallel Bloom Hit System'i teslim etti (commit `3948c2c`, 54/54 test PASS). Bundan sonraki adımlar: review, profil, yeni E2E, test.

---

## 2. Mevcut Durum

| Task | ID | Durum |
|:-----|:---|:------|
| Phase 2: Parallel Bloom Hit | `t_8be60281` | ⊘ blocked (review-required) |
| Task 3: E2E (eski) | `t_47ab7950` | 🗄 archived |
| memory-freak profili | — | ✅ hazır, hiç çalışmadı |
| Yeni E2E task'ı | — | ❌ açılmadı |

**Mad-man'ın teslim ettiği:**
- `features/bloom/types.go` — +BloomFullURL, depth weight 1.5
- `features/bloom/url_parser.go` — File detection (path.Ext), parentPaths(), GenerateCheckKeys(), CheckKey
- `features/bloom/manager.go` — PopulateEntry(), determineBloomTarget(), parallel Likely() + context cancel
- `features/bloom/bloom_test.go` — 14 unit test
- `features/tests/blacked_integration_test.go` — 40 test güncellendi

**Mad-man'ın kendi fark ettiği:**
- `parentPaths()` full path segment'i de dahil ediyor (planda sadece parent dizinler vardı) — işlevsel olarak zararsız ama gereksiz goroutine
- `BloomPath`/`BloomQuery` sabitleri `types.go`'da duruyor ama kullanılmıyor — ölü kod, risk yok
- Kendi testini (`TestParentPath_Match`) önce yanlış yönde yazıp sonra düzeltmiş

---

## 3. Adımlar

### Step 1: Review Mad-man's Code

Mad-man `review-required` bloğu koydu. Kod review yapılacak:

- [ ] `/api/v1/check` endpoint'i hâlâ çalışıyor mu? (canlı test)
- [ ] `pond_collector.go` güncellendi mi? (mad-man kontrol etmiş, `bm.Add` kalmamış)
- [ ] `parentPaths`'in full path dahil etmesi optimize edilmeli mi?
- [ ] `BloomPath`/`BloomQuery` constant'ları temizlenmeli mi? (başka import var mı?)
- [ ] `determineBloomTarget`'da `Host==Domain` → Domain bloom kararı doğru mu?

### Step 2: Memory-Freak Profiling

memory-freak profili dispatch edilecek. Şunları profiller:

- [ ] **Bloom set insert/check latency** — her bloom tipi için `go test -bench=.`
- [ ] **Parallel Likely() goroutine overhead** — context cancel + goroutine spawn maliyeti
- [ ] **Memory alloc per bloom check** — `-benchmem` ile alloc sayısı
- [ ] **RebuildSource performansı** — provider sync sonrası bloom rebuild süresi
- [ ] **Http handler latency** — `/api/v1/check` ve `/api/v1/hit` için p50/p95/p99

### Step 3: Yeni E2E Test Suite

Bloom refactor'dan sonra eski E2E'ler geçersiz. Yeni E2E'ler şunları kapsamalı:

**Populate doğrulama (her bloom tipi için):**
- [ ] File+Query → FullURL bloom
- [ ] File (query'siz) → File bloom
- [ ] HostPath (uzantısız path) → HostPath bloom
- [ ] Bare domain → Domain bloom
- [ ] Subdomain → Host bloom
- [ ] IP → IP bloom

**Check doğrulama (zincir sırası):**
- [ ] Domain hit → Host/HostPath/File/FullURL'e bakma
- [ ] Host hit → altındakilere bakma
- [ ] HostPath parent match (/a populate, /a/b/c check → hit)
- [ ] File hit (query fark etmez, sadece filename)
- [ ] FullURL query exact match
- [ ] Query farklıysa MISS (sağlayıcı sorumluluğu)
- [ ] Farklı subdomain → MISS
- [ ] Var olmayan domain → MISS

**Provider sync + E2E:**
- [ ] Provider sync çalıştır → bloom populate
- [ ] Sync sonrası HTTP API'den sorgula
- [ ] Re-sync stabilitesi (ikinci sync sonrası aynı sonuçlar)
- [ ] Sadece HTTP API kullan, internal fonksiyon çağırma

**Test dosyası:** `features/e2e/bloom_e2e_test.go`
**Branch:** feat/blacked-mvp

### Step 4: Canlı Test

Runaho manuel test edecek — bunun için:
- [ ] Server çalışır durumda
- [ ] Insomnia export'u hazır (`docs/insomnia-export.json`)
- [ ] Test URL'leri tanımlı

---

## 4. Değişecek/Silinecek Dosyalar

| Dosya | İşlem |
|:------|:------|
| `features/e2e/bloom_e2e_test.go` | **Yeni** — E2E test suite |
| `features/e2e/testdata/e2e_urls.json` | **Yeni** — test URL verileri |
| `features/bloom/types.go` | 🔍 BloomPath/BloomQuery temizlik? |
| `features/bloom/url_parser.go` | 🔍 parentPaths optimizasyonu? |

---

## 5. Açık Sorular

1. **Review ne kadar derin?** — Mad-man'ın kodunu satır satır mı inceleyelim, yoksa "testler geçiyor, güven" diyip memory-freak'a mı geçelim?
2. **memory-freak task'ı dispatch edelim mi?** — Yoksa review'dan sonra mı?
3. **E2E'yi yeni task olarak açalım mı?** — `t_8be60281`'in child'ı mı olsun, bağımsız mı?
4. **E2E kim yazsın?** — Mad-man geri çağrılıp yazdıralım mı, yoksa ben doğrudan yazayım mı?

---

## 6. Risks

1. **Review atlama riski** — Mad-man'ın `parentPaths` full path hatası küçük ama eğer hiç review yapmazsak ileride debug'ı zor olabilir.
2. **E2E olmadan manuel test** — Runaho "ben test edeceğim" dedi ama E2E olmadan regression'ı yakalamak zor.
3. **memory-freak sırası** — Bloom refactor'dan hemen sonra profil almak mantıklı, çünkü parallel goroutine pattern'i performansı etkilemiş olabilir.
