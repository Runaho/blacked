# Blacked — Pazar Araştırması Raporu

**Tarih:** 18 Mayıs 2026
**Kapsam:** Rekabet analizi, threat intel provider ekosistemi, self-hosted alternatifler, pazar büyüklüğü, buyer persona, konumlandırma

---

## 1. Rekabet Analizi: URL Tarama & Blacklist Servisleri

### 1.1 URLScan.io

| Özellik | Detay |
|---------|-------|
| **Model** | SaaS (bulut), headless Chromium sandbox |
| **Fiyat** | Free ($0, 100 scan/ay, public), Pro ($49/ay, 10K scan), Enterprise (custom) |
| **Hedef Kitle** | SOC analistleri, TI araştırmacıları, pentester'lar |
| **Güçlü** | Zengin veri (ağ trafiği + JS + screenshot), public feed, kolay UI |
| **Zayıf** | Public scan zorunlu (Free), rate limit düşük (10 req/min Free, 100 Pro), batch scanning yok, offline çalışmaz |
| **Blacked farkı** | Dinamik değil → tamamlayıcı. Blacked hızlı ön eleme, URLScan deep analiz |

### 1.2 VirusTotal

| Özellik | Detay |
|---------|-------|
| **Model** | SaaS (Google bünyesinde), 70+ AV motoru + URL/domain reputation |
| **Fiyat** | Free (500 req/gün), Intelligence (~$300/ay), Enterprise (custom) |
| **Güçlü** | Dev motor ağı, hızlı yanıt, topluluk verisi |
| **Zayıf** | **Veri gizliliği sıfır** — taranan URL'ler herkese açık. Rate limit çok düşük (1 req/dk). Büyük hacimde pahalı |
| **Blacked farkı** | Self-hosted = veri dışarı çıkmaz. Sınırsız rate. Altyapı maliyeti sabit |

### 1.3 Karşılaştırma Matrisi

| Kriter | URLScan.io | VirusTotal | AbuseIPDB | **Blacked** |
|--------|-----------|------------|-----------|-------------|
| **Self-hosted** | ❌ | ❌ | ❌ | ✅ |
| **Veri gizliliği** | ❌ (Free public) | ❌ (public) | ⚠️ | ✅ Tam |
| **Lookup hızı** | 10-30sn | <1sn | <1sn | **<1ms** (Bloom) |
| **Rate limit** | 10-100 req/min | 1 req/dk | 1000/gün free | Sınırsız |
| **Batch API** | ❌ | ❌ | ❌ | ✅ |
| **Offline** | ❌ | ❌ | ❌ | ✅ |
| **Multi-layer match** | ❌ | ❌ | ❌ | ✅ |
| **Scoring engine** | ❌ | ⚠️ (VT score) | ⚠️ (abuse) | ✅ |
| **Fiyat** | $0-500+/ay | $0-5000+/ay | $0-100+/ay | **Altyapı maliyeti** |

---

## 2. Provider Ekosistemi (Blacked'e Entegre Edilebilir)

| Provider | Tür | API | Ücret | Veri Kalitesi | Entegrasyon |
|----------|-----|-----|-------|---------------|-------------|
| **OISD_BIG** ✅ | Domain/IP blacklist | Feed | Ücretsiz | Yüksek | **Mevcut** |
| **URLHAUS** ✅ | Malware URL | Feed | Ücretsiz | Yüksek | **Mevcut** |
| **OPENPHISH** ✅ | Phishing URL | Feed | Ücretsiz | Yüksek | **Mevcut** |
| **PhishTank** 🔜 | Phishing URL | API | Ücretsiz | Orta | Döküm al + periyodik sync |
| **AbuseIPDB** 🔜 | IP blacklist | API | Free (1000/gün) / $20-100/ay | İyi | API + cache katmanı |
| **AlienVault OTX** 🔜 | Multi-IoC (IP, domain, hash, CVE) | API | Ücretsiz (10K/gün) | Değişken | En geniş kapsam, pulse takibi |
| **ThreatFox** 🔜 | Malware IOC | API | Ücretsiz | Yüksek | Gerçek malware C2'ler |

**Önerilen roadmap:** OTX → ThreatFox → PhishTank → AbuseIPDB sırasıyla provider eklenmeli. OTX en geniş kapsamı verir, ThreatFox en kaliteli malware IOC'leri sağlar.

---

## 3. Self-Hosted Alternatifler ve Boşluk Analizi

### DNS-level tools

| Araç | Yaklaşım | Blacked'den Farkı |
|------|----------|-------------------|
| **Pi-hole** | DNS sinkhole, domain bazlı bloklama | Sadece tüketici, TI üretmez, URL seviyesi yok |
| **AdGuard Home** | DNS+HTTPS filtreleme | Pi-hole'a benzer, biraz daha gelişmiş ama URL seviyesi yok |
| **dnscrypt-proxy** | DNS şifreleme + opsiyonel blacklist | Blacklist yan özellik, zayıf |

### TI platformları

| Araç | Yaklaşım | Blacked'den Farkı |
|------|----------|-------------------|
| **MISP** | IoC paylaşımı, taksonomi, galaxies | **Ağır** — kurulum/operasyon karmaşık. TI paylaşımı odaklı, hızlı lookup engine değil |
| **OpenCTI** | STIX/TAXII, grafik model, varlıklar | Genel TI. Bloom filter yok, hızlı query engine yok |
| **Yeti** | GraphQL TI, enrichment | MISP'den hafif ama yine TI platformu, blacklist engine değil |
| **TheHive/Cortex** | IR + enrichment | Case management aracı, blacklist değil |

### Blacked'in doldurduğu boşluk

Hiçbiri şu kombinasyonu sunmuyor:
- **Bloom filter + multi-layer matching** (domain → host → host_path → file → full_url)
- **Scoring engine** (trust × weight × match depth)
- **Self-hosted, MIT licensed** (veri dışarı çıkmaz)
- **Provider agnostic** (birden çok kaynaktan beslenir)
- **Hızlı lookup** (<1ms, milyonlarca entry'de)

**Özet:** Pi-hole tüketicidir, MISP üreticidir ama ağırdır. Blacked ikisinin ortasında — **hafif, hızlı, self-hosted blacklist engine**.

---

## 4. Pazar Büyüklüğü ve Trendler

### Threat Intelligence pazarı
- **Büyüklük:** $12-15 milyar (2025)
- **CAGR:** ~%17-19 (2030'da $30-38 milyar)
- **URL scanning/blacklist segmenti:** ~$2-3 milyar (2025), %20+ büyüme

### Trendler
- **Bulut baskın (%75-80)** — VirusTotal, URLScan.io, AbuseIPDB gibi
- **Self-hosted yükseliyor** — GDPR/KVKK/veri gizliliği baskısıyla. Özellikle finans, kamu, sağlık sektörü self-hosted talep ediyor
- **Hibrit model** — buluttan feed çek, local engine'de sorgula (Blacked'in tam doğal konumu)
- **AI/ML threats artıyor** — phishing kampanyaları hızlanıyor, daha hızlı blacklist güncellemesi gerekiyor

---

## 5. Buyer Persona & Pazar Segmentleri

### Segment 1: SOC/Güvenlik Mühendisleri (Birincil)
- **Kim:** SOC analisti, güvenlik mühendisi, TI araştırmacısı
- **Pain point:** VT/URLScan.io'ya her sorguda para ödemek, rate limit, verinin 3. partiye gitmesi
- **Ne arar:** Hızlı lookup, self-hosted, SIEM entegrasyonu, öngörülebilir maliyet
- **Ne öder:** Kendi sunucusunda çalıştıracağı için $0 yazılım — ama hosted/enterprise versiyon olursa $500-2000/yıl

### Segment 2: MSSP/MSP'ler
- **Kim:** Müşterilerine güvenlik hizmeti satan firmalar
- **Pain point:** Her müşteri için ayrı API anahtarı, toplu maliyet
- **Ne arar:** Multi-tenant, self-hosted, white-label
- **Ne öder:** $2000-10000/yıl (hosted/enterprise tier)

### Segment 3: DNS Filter / Email Security Firmalar
- **Kim:** Pi-hole/AdGuard yöneten ekipler, email güvenliği sağlayıcıları
- **Pain point:** Mevcut blacklist'ler güncel değil, false positive oranı yüksek
- **Ne arar:** Programatik API, yüksek throughput, düşük latency
- **Not:** Blacked'i internal bileşen olarak kullanırlar, direkt "müşteri" olmazlar

### Segment 4: Açık Kaynak Topluluğu (Büyüme Kaldıracı)
- **Kim:** Kendi güvenlik altyapısını kuran geliştiriciler
- **Ne arar:** MIT lisansı, Go codebase, kolay kurulum
- **Değer:** Topluluk → contributor → brand awareness → enterprise satış

---

## 6. Blacked Konumlandırma Önerisi

### One-liner
> **Self-hosted URL blacklist engine** — hızlı, özel, ölçeklenebilir.

### Positioning
| Mevcut çözüm | Problemi | Blacked |
|--------------|----------|---------|
| VirusTotal | Pahalı, rate limited, veri sızdırıyor | Self-hosted, sınırsız, gizli |
| Pi-hole | Sadece domain, TI kaynağı değil | URL+domain+path+full, provider agnostic |
| MISP/OpenCTI | Ağır, kurulum karmaşık | Hafif, Go binary, tek dosya |
| AbuseIPDB | Sadece IP | URL+domain+IP+path+full |

### Open-core / Commercial Model

**Open source (MIT):**
- Temel engine: provider sync + Bloom filter + scoring + CLI + REST API
- Topluluk provider'ları (OISD, URLHAUS, OPENPHISH)

**Enterprise (ücretli):**
- Premium provider'lar (AbuseIPDB, OTX, ThreatFox) — önceden yapılandırılmış
- Multi-tenant dashboard
- SIEM/SOAR connector'ları
- Managed update service
- SLA desteği

**Pricing benchmark:**
| Alternatif | Aylık | Yıllık | Blacked karşılığı |
|-----------|-------|--------|-------------------|
| URLScan.io Pro | $49 | $588 | Self-hosted = 1 sunucu ~$10/ay |
| VT Intelligence | ~$300 | ~$3600 | Kendi engine'in |
| AbuseIPDB Pro | $20 | $240 | Provider modülü |

---

## 7. Stratejik Öneriler

### Kısa Vade (Mevcut MVP)
1. **Provider sayısını artır** — OTX ve ThreatFox ekle, veri havuzunu büyüt
2. **Dökümantasyon** — Kurulum + API dökümanı, GitHub'da showcase
3. **Benchmark** — Blacked vs VirusTotal/AbuseIPDB hız/doğruluk/maliyet karşılaştırması yayınla

### Orta Vade (v0.3-v0.5)
1. **Topluluk feed'i** — Kullanıcıların kendi provider eklemesine izin ver
2. **SIEM/SOAR entegrasyonu** — Splunk, ELK, TheHive connector'ları
3. **False-positive yönetimi** — Whitelist override, feedback loop
4. **Landing page + docs** — `blacked.dev` gibi bir site

### Uzun Vade
1. **Hosted version** — "Blacked Cloud" (ücretli)
2. **Enterprise özellikler** — Multi-tenant, RBAC, audit log
3. **Topluluk** — GitHub stars → contributors → word of mouth

---

## 8. Riskler ve Dikkat Edilmesi Gerekenler

| Risk | Olasılık | Etki | Mitigasyon |
|------|----------|------|------------|
| VT/Google benzer ürün çıkarır | Düşük | Yüksek | Self-hosted + niche odak — VT self-hosted yapmaz |
| MISP blacklist engine ekler | Orta | Orta | MISP zaten ağır, hızlı lookup eklemesi zor |
| AbuseIPDB self-hosted açar | Düşük | Düşük | Onlar IP odaklı, Blacked URL+multi-layer |
| Topluluk ilgisi az olur | Orta | Orta | Enterprise geliriyle sürdür, os community beklentisini düşük tut |
| Yasal/büyüme: provider API'ları kısıtlama getirir | Orta | Orta | Her provider için fallback plan, local cache |

---

## Kaynaklar

- URLScan.io pricing page
- VirusTotal pricing & API docs
- AbuseIPDB pricing page
- MarketsandMarkets Threat Intelligence Market Report
- Grand View Research — TI market analysis
- Gartner — Self-hosted TI trends
- MISP, OpenCTI, Pi-hole, AdGuard Home — official documentation
