# Blacked — Provider Entegrasyon Planı

> Tarih: 2026-05-18
> Kapsam: URL/IP blacklist aggregator için rakip/alternatif kaynak analizi ve entegrasyon roadmap'i
> Durum: Planlama aşaması

---

## 📋 İçindekiler

1. [Mevcut Durum (Blacked Provider'ları)](#1-mevcut-durum)
2. [Kategori 1: Feed/Liste Çekenler — Doğrudan Provider](#2-kategori-1-feedliste-cekenler)
3. [Kategori 2: API Tabanlı — Ücretli/Sınırlı Ama Yüksek Değer](#3-kategori-2-api-tabanli)
4. [Kategori 3: Go Referans Projeleri — Kod/Mimari İçin](#4-kategori-3-go-referans-projeleri)
5. [Kategori 4: Blacked'deki Kritik Eksikler (Feature Gap)](#5-kategori-4-blackeddeki-kritik-eksikler)
6. [Entegrasyon Roadmap'i](#6-entegrasyon-roadmap)
7. [Özet Tablo](#7-ozet-tablo)

---

## 1. Mevcut Durum

Blacked'de halihazırda **4 provider** bulunuyor:

| Provider | Tür | Çekme Yöntemi | Veri Formatı | Kapsam |
|----------|:---:|:-------------:|:------------:|:------:|
| **OISD** (big + nsfw) | Domain blacklist | HTTP GET | Düz domain listesi (20K+) | Reklam, tracker, malware domain |
| **OpenPhish** | Phishing feed | HTTP GET | RSS feed | Güncel phishing URL'leri |
| **PhishTank** | Phishing validation | HTTP POST (online) | JSON API | Phishing URL doğrulama |
| **URLhaus** | Malware URL | HTTP GET | JSON API | Malware barındıran URL'ler |

**Hepsi domain/URL bazlı.** IP sorgulama, CIDR eşleme veya online reputation API'si **yok.**

---

## 2. Kategori 1: Feed/Liste Çekenler

Bunlar düz HTTP ile liste veya JSON feed çeken, **provider olarak eklenmesi en kolay** kaynaklar.

---

### 2.1 ThreatFox (abuse.ch)

| Özellik | Detay |
|---------|-------|
| **URL** | `https://threatfox.abuse.ch` |
| **Feed** | `https://threatfox.abuse.ch/export/json/` |
| **API Docs** | `https://threatfox.abuse.ch/api/` |
| **Auth** | Yok (public feed) |
| **Rate Limit** | Bilinmiyor (makul kullanım) |
| **Veri** | IOC: IP, domain, URL, hash |
| **Format** | JSON (IOC list) |
| **Güncelleme** | Düzenli, günde birkaç kez |
| **Blacked Pattern** | URLhaus ile neredeyse aynı |
| **Öncelik** | 🟢 Çok yüksek |

**Entegrasyon Notları:**
- `/export/json/` endpoint'inden full IOC listesi çekilir
- Her IOC'de: `ioc_type` (ip/domain/url), `threat_type` (payload_delivery/c2/banking), `malware_printable`
- URLhaus provider'ındaki `urlhaus_abuse.go` birebir referans — aynı API yapısı
- Filter: sadece `threat_type` in (payload_delivery, c2, malware) olanları al

**Provider Dosyası:** `features/providers/threatfox/threatfox.go`

---

### 2.2 BlockList.de

| Özellik | Detay |
|---------|-------|
| **URL** | `https://www.blocklist.de` |
| **Feed** | `https://www.blocklist.de/de/export/` |
| **API Docs** | `https://www.blocklist.de/en/export.html` |
| **Auth** | Yok (public) |
| **Rate Limit** | Yok |
| **Veri** | 30K+ IP (fail2ban saldırganları) |
| **Format** | Düz text (IP list, line-by-line) |
| **Güncelleme** | Gerçek zamanlı (fail2ban) |
| **Öncelik** | 🟢 Çok yüksek |

**Feed URL'leri:**
- SSH: `https://www.blocklist.de/downloads/ssh.txt`
- Mail: `https://www.blocklist.de/downloads/mail.txt`
- Apache: `https://www.blocklist.de/downloads/apache.txt`
- FTP: `https://www.blocklist.de/downloads/ftp.txt`
- SIP: `https://www.blocklist.de/downloads/sip.txt`
- Botnet: `https://www.blocklist.de/downloads/bots.txt`
- Kombine: `https://lists.blocklist.de/lists/all.txt`

**Entegrasyon Notları:**
- Sadece IP listesi (line-by-line), parse etmesi en kolay provider
- Her IP'ye `blacklist` type, kaynak olarak `blocklist_de` tag'i
- **CIDR/IP altyapısı olmadan parse edilemez** — önce Phase 1'de CIDR altyapısı lazım
- `all.txt` kombine feed tek bir HTTP çağrısıyla tüm IP'leri alır

**Provider Dosyası:** `features/providers/blocklistde/blocklistde.go`

---

### 2.3 Spamhaus DROP / EDROP

| Özellik | Detay |
|---------|-------|
| **URL** | `https://www.spamhaus.org` |
| **DROP Feed** | `https://www.spamhaus.org/drop/drop.txt` |
| **EDROP Feed** | `https://www.spamhaus.org/drop/edrop.txt` |
| **ASL Feed** | `https://www.spamhaus.org/drop/asn.txt` |
| **Auth** | Yok (public) |
| **Rate Limit** | Yok |
| **Veri** | Kötü niyetli IP netblock'ları (CIDR notation) |
| **Format** | Text: `NETBLOCK_CIDR ; description` |
| **Güncelleme** | Günde 1-2 kez |
| **Öncelik** | 🟢 Yüksek |

**Örnek Veri:**
```
1.2.0.0/16 ; Some description
10.0.0.0/8 ; Another entry
```

**Entegrasyon Notları:**
- **CIDR formatında** gelir — `cidranger` lib ile parse edilip sorgulanmalı
- DROP = "Don't Route Or Peer" (kötü niyetli netblock'lar)
- EDROP = DROP'a ek extended list
- `;` ile ayrılmış, first field CIDR, second field açıklama
- Spamhaus 10+ yıldır sektör standardı — eklemek credibility kazandırır

**Provider Dosyası:** `features/providers/spamhaus/spamhaus.go`

---

### 2.4 Phishing.Database (mitchellkrogza)

| Özellik | Detay |
|---------|-------|
| **URL** | `https://github.com/mitchellkrogza/Phishing.Database` |
| **Feed** | `https://raw.githubusercontent.com/mitchellkrogza/Phishing.Database/master/phishing-links.csv` |
| **Auth** | Yok (public GitHub) |
| **Rate Limit** | GitHub raw limiti (makul) |
| **Veri** | 10M+ phishing URL |
| **Format** | CSV (URL, status, date) |
| **Güncelleme** | Düzenli (bot ile güncelleniyor) |
| **Öncelik** | 🟢 Yüksek |

**Entegrasyon Notları:**
- CSV parse — ilk kolon URL, ikinci kolon online/offline status
- Sadece "online" olanları al (canlı phishing)
- Büyük liste (10M+) — ilk çekimde chunk'la, sonra diff update
- GitHub raw'dan HTTP GET ile çekilir
- OpenPhish'e tamamlayıcı — farklı kaynaktan phishing URL

**Provider Dosyası:** `features/providers/phishingdb/phishingdb.go`

---

### 2.5 Pi-hole Adlist (Firebog / W3K)

| Özellik | Detay |
|---------|-------|
| **URL** | `https://firebog.net` (toplu liste) |
| **Ticked List** | `https://v.firebog.net/hosts/lists.php?type=tick` |
| **W3K List** | `https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts` |
| **Auth** | Yok |
| **Rate Limit** | Yok |
| **Veri** | 100K+ domain (reklam, tracker, malware) |
| **Format** | Hosts dosyası (0.0.0.0 domain) veya domain listesi |
| **Güncelleme** | Haftalık |
| **Öncelik** | 🟡 Orta |

**Entegrasyon Notları:**
- Hosts formatı (`0.0.0.0 domain.tld`) parse edilir
- Firebog'un ticked list'i en popüler 80+ adlist'i içerir — tek URL'den çoklu kaynak
- OISD'ye alternatif/tamamlayıcı — farklı kaynaklardan domain
- Düşük öncelik çünkü OISD zaten bu kapsamın çoğunu karşılıyor

**Provider Dosyası:** `features/providers/pihole/pihole.go`

---

### 2.6 AdGuard DNS Filters

| Özellik | Detay |
|---------|-------|
| **URL** | `https://adguard.com/adguard-home/overview.html` |
| **Filter URL** | `https://adguardteam.github.io/AdGuardSDNSFilter/Filters/filter.txt` |
| **Auth** | Yok |
| **Rate Limit** | Yok |
| **Veri** | 50K+ domain (tracker, phishing, malware) |
| **Format** | AdBlock formatı ( `||domain^` ) |
| **Güncelleme** | Düzenli |
| **Öncelik** | 🟡 Orta |

**Entegrasyon Notları:**
- AdBlock format (`||domain.tld^`) — `domain` kısmını extract et
- OISD ile örtüşme yüksek, sadece farklı kaynaktan cross-check için değerli
- Düşük öncelik

---

## 3. Kategori 2: API Tabanlı

Bunlar REST API gerektirir, rate-limited ve genelde ücretli. Ama **online reputation** için en değerli kaynaklar.

---

### 3.1 VirusTotal

| Özellik | Detay |
|---------|-------|
| **URL** | `https://www.virustotal.com` |
| **API** | `https://www.virustotal.com/api/v3/` |
| **Auth** | API Key (ücretsiz kayıt) |
| **Rate Limit** | Public: 4 req/dk (500/gün) |
| **Pricing** | Premium: $1000/ay |
| **Veri** | URL scan, IP reputation, file hash, domain report |
| **Format** | JSON (REST API) |
| **Öncelik** | 🟠 Orta (ücretli ama çok değerli) |

**Entegrasyon Notları:**
- Online scan endpoint: `POST /api/v3/urls` (URL submit), `GET /api/v3/analyses/{id}` (result)
- URL rep: `GET /api/v3/urls/{url_id}` — community score
- IP rep: `GET /api/v3/ip_addresses/{ip}` — 90+ engine sonucu
- Public API çok kısıtlı (4 req/dk) — sadece premium'da anlamlı
- Community API'den `last_analysis_stats` (harmless, malicious, suspicious) alınır
- Blacked'de "online verification" pipeline'ına eklenebilir

---

### 3.2 AbuseIPDB

| Özellik | Detay |
|---------|-------|
| **URL** | `https://www.abuseipdb.com` |
| **API** | `https://api.abuseipdb.com/api/v2/` |
| **Auth** | API Key (ücretsiz kayıt) |
| **Rate Limit** | 1000 sorgu/gün (ücretsiz) |
| **Pricing** | Pro: $40-200/ay |
| **Veri** | IP reputation score + rapor kategorileri |
| **Format** | JSON (REST API) |
| **Öncelik** | 🟡 Yüksek (1000/gün ücretsiz) |

**Entegrasyon Notları:**
- Endpoint: `GET /api/v2/check?ipAddress={ip}`
- Response: `{ "data": { "abuseConfidenceScore": 100, "countryCode": "US", "usageType": "Web Hosting", "isp": "..." } }`
- PhishTank `online_valid.go` pattern'ı birebir — aynı "online verification" yapısı
- Blacked'de her IP sorgusunda AbuseIPDB'e de sor → anlık doğruluk
- 1000/gün küçük projeler için yeterli

---

### 3.3 urlscan.io

| Özellik | Detay |
|---------|-------|
| **URL** | `https://urlscan.io` |
| **API** | `https://urlscan.io/api/v1/` |
| **Docs** | `https://urlscan.io/docs/api/` |
| **Auth** | API Key (ücretsiz kayıt) |
| **Rate Limit** | Public: 50 sorgu/gün |
| **Pricing** | Pro: $200/ay |
| **Veri** | URL scan (screenshot, DOM, redirect chain, verdict) |
| **Format** | JSON (REST API) |
| **Öncelik** | 🔴 Düşük (pahalı, kısıtlı) |

**Entegrasyon Notları:**
- Scan submit: `POST /api/v1/scan/` → URL'i taratır
- Result: `GET /api/v1/result/{uuid}` — full scan raporu
- Veri çok zengin (redirect chain, HTTP haritası, DOM), ama rate limit çok düşük
- Sadece premium'da anlamlı bir entegrasyon
- "Detected URL" listesinden de public feed çekilebilir (`/api/v1/identicons/`)

---

### 3.4 IPQualityScore

| Özellik | Detay |
|---------|-------|
| **URL** | `https://www.ipqualityscore.com` |
| **API** | `https://ipqualityscore.com/api/json/ip/{key}/{ip}` |
| **Auth** | API Key |
| **Pricing** | $0.005/sorgu ($10-100/ay) |
| **Veri** | IP fraud score, proxy/VPN detection, risk factors |
| **Format** | JSON (REST API) |
| **Öncelik** | 🔴 Düşük (tam ücretli) |

**Entegrasyon Notları:**
- Proxy/VPN/VPS tespiti — AbuseIPDB'den farklı bir vektör
- `fraud_score: 0-100`, `proxy: bool`, `vpn: bool`, `tor: bool`
- Tam ücretli, ücretsiz katman yok
- Sadece premium Blacked deployment'larında anlamlı

---

### 3.5 MaxMind (GeoIP + Fraud)

| Özellik | Detay |
|---------|-------|
| **URL** | `https://www.maxmind.com` |
| **DB** | GeoLite2 (ücretsiz) / GeoIP2 ($500-5000/ay) |
| **Auth** | API Key (ücretsiz DB) |
| **Veri** | IP → country, city, ISP, risk score |
| **Format** | MMDB (binary DB file) |
| **Öncelik** | 🔴 Düşük (öncelik değil) |

**Entegrasyon Notları:**
- mmdb format — Go'da `github.com/oschwald/maxminddb-golang` ile okunur
- Blacked'de IP coğrafi verisi "güzel ama kritik değil"
- GeoLite2 ücretsiz ama country-level; premium city+ISP+risk verir
- CIDR altyapısından sonra düşünülebilir

---

## 4. Kategori 3: Go Referans Projeleri

Bunlar provider değil, **Blacked'in kendi kodunda kullanılabilecek Go kütüphaneleri ve referans projeler.**

---

### 4.1 yl2chen/cidranger

| Özellik | Detay |
|---------|-------|
| **Proje** | `https://github.com/yl2chen/cidranger` |
| **Dil** | Go |
| **Ne İşe Yarar** | CIDR routing tree — IP'nin hangi netblock'ta olduğunu bulma |
| **Blacked'de Yeri** | `/v2/check?ip=1.2.3.4` endpoint'inde kullanılacak |
| **Öncelik** | 🔴 **CRITICAL — olmazsa olmaz** |

**Kullanım:**
```go
import "github.com/yl2chen/cidranger"

ranger := cidranger.NewRanger()
_, network, _ := net.ParseCIDR("1.2.0.0/16")
ranger.Insert(cidranger.NewBasicRangerEntry(*network))

// Sorgu:
contains, err := ranger.Contains(net.ParseIP("1.2.3.4"))
// contains = true
```

**Neden Gerekli:**
- BlockList.de, Spamhaus, ThreatFox — hepsi IP/CIDR veriyor
- Blacked'de IP sorgulama için CIDR matching altyapısı şart
- `cidranger` en performant Go CIDR kütüphanesi (radix tree tabanlı)

---

### 4.2 jehiah/blacklist

| Özellik | Detay |
|---------|-------|
| **Proje** | `https://github.com/jehiah/blacklist` |
| **Dil** | Go |
| **Ne İşe Yarar** | DNSBL + regex + URL blacklist check |
| **Blacked'de Yeri** | Query mantığı referansı |

**Alınacak Dersler:**
- DNSBL (DNS-based blackhole list) sorgulama — Blacked'de şu an yok
- Regex bazlı URL pattern matching
- Mevcut Blacked query sistemiyle büyük ölçüde örtüşüyor

---

### 4.3 bitly/dablooms

| Özellik | Detay |
|---------|-------|
| **Proje** | `https://github.com/bitly/dablooms` |
| **Dil** | C (Go wrapper gerek) |
| **Ne İşe Yarar** | Counting bloom filter — yüksek performans, scalable |
| **Blacked'de Yeri** | Bloom filter scaling stratejisi referansı |

**Alınacak Dersler:**
- Counting bloom = silme desteği (Blacked'de şu an yok)
- Memory-page'li scaling (büyük set'ler için)
- Mevcut bloom implementasyonuna alternatif yaklaşım

---

### 4.4 tylerbrock/url-bloom

| Özellik | Detay |
|---------|-------|
| **Proje** | `https://github.com/tylerbrock/url-bloom` |
| **Dil** | Go |
| **Ne İşe Yarar** | URL → bloom filter, multiple hash functions |
| **Blacked'de Yeri** | Bloom hash stratejisi referansı |

---

### 4.5 lloyd/xxhash-bloom

| Özellik | Detay |
|---------|-------|
| **Proje** | `https://github.com/lloyd/xxhash-bloom` |
| **Dil** | Go |
| **Ne İşe Yarar** | xxhash tabanlı bloom filter — çok hızlı hash |
| **Blacked'de Yeri** | Hash fonksiyonu seçimi referansı |

---

### 4.6 InQuest/ThreatIngestor

| Özellik | Detay |
|---------|-------|
| **Proje** | `https://github.com/InQuest/ThreatIngestor` |
| **Dil** | Python (mimari referans) |
| **Ne İşe Yarar** | OSINT toplama framework: RSS, API, Twitter → normalize |
| **Blacked'de Yeri** | Provider pipeline mimarisi |

**Alınacak Dersler:**
- Config-driven provider yönetimi
- Normalize edilmiş IOC formatına dönüşüm
- Scheduler + worker pattern

---

### 4.7 Neo23x0/Loki

| Özellik | Detay |
|---------|-------|
| **Proje** | `https://github.com/Neo23x0/Loki` |
| **Dil** | Python |
| **Ne İşe Yarar** | IOC scanner: YARA, hash, domain bazlı tarama |
| **Blacked'de Yeri** | IOC format referansı, olası YARA entegrasyon fikri |

---

### 4.8 MISP (Malware Information Sharing Platform)

| Özellik | Detay |
|---------|-------|
| **Proje** | `https://www.misp-project.org` |
| **Dil** | PHP + Python (API) |
| **Ne İşe Yarar** | Threat intelligence paylaşım platformu |
| **Blacked'de Yeri** | Entegrasyon hedefi — MISP'den IOC çekme |

**Not:** MISP kendi başına bir platform, provider değil. Ama ileride Blacked'in MISP feed'lerinden IOC çekmesi mümkün.

---

## 5. Kategori 4: Blacked'deki Kritik Eksikler

### 5.1 IP / CIDR Sorgulama Desteği 🚨 #1

**Mevcut Durum:** Blacked sadece **domain + URL** bazlı sorgulama yapıyor. IP girdiğinde hiçbir şey dönmez.

**Çözüm:**
1. `cidranger` Go kütüphanesini `go.mod`'a ekle
2. Provider'lar IP/CIDR data parse edip ranger'a eklesin
3. Yeni endpoint: `GET /v2/check?q=1.2.3.4` → IP sorgulama
4. Mevcut `/v2/check?q=domain.com` zaten var

**Provider'ların IP Formatı:**
- BlockList.de: düz IP (line-by-line)
- Spamhaus DROP: CIDR notation (`1.2.0.0/16`)
- ThreatFox: hem IP hem CIDR
- AbuseIPDB: API'den IP bazlı sonuç

**Gerekli Dosyalar:**
- `internal/network/ip_checker.go` — CIDR ranger wrapper
- `features/entries/enums/query_type.go`'a `IP` tipi eklenecek

---

### 5.2 Threat Intelligence Feed Çeşitliliği 🚨 #2

**Mevcut Durum:** 4 provider, tamamı domain/URL odaklı.

**Hedef:** 4 → 8+ provider, her biri farklı threat vector:

| Vector | Mevcut | Eklenecek |
|--------|:------:|:---------:|
| Phishing | OpenPhish, PhishTank | Phishing.Database |
| Malware URL | URLhaus | ThreatFox |
| Malware IP | — | BlockList.de, Spamhaus |
| Genel IOC | — | AlienVault OTX |
| Domain | OISD | Pi-hole, AdGuard |
| Online Rep | PhishTank | AbuseIPDB, VirusTotal |

---

### 5.3 Online Reputation API 🚨 #3

**Mevcut Durum:** Sadece PhishTank online doğrulama yapıyor. Diğer tüm provider'lar statik liste bazlı.

**Çözüm:**
- AbuseIPDB: PhishTank `online_valid.go` pattern'ı ile eklenebilir
- VirusTotal: Community API'den 4 req/dk — sadece premium'da anlamlı
- urlscan.io: 50 req/gün

**Pipeline:**
```
İstek gelir → Bloom filter (L1) → Badger cache (L2) → sorgulanamazsa AbuseIPDB/VT'ye sor → cache'e ekle
```

---

### 5.4 Regex / Pattern Bazlı Blacklist

**Mevcut Durum:** Exact domain match + suffix match.

**Potansiyel:**
- `jehiah/blacklist` regex desteği var
- `^https?://.*\.xyz/.*$` gibi TLD bazlı pattern'ler
- Domain pattern matching (ör: `*-tracker.example.com`)

**Öncelik:** Düşük. Önce IP/CIDR + feed çeşitliliği.

---

### 5.5 Bloom Filter İyileştirmeleri

**Mevcut Durum:** Counting bloom değil, silme desteği yok.

**Potansiyel:**
- `dablooms` counting bloom — entry silme
- `xxhash-bloom` — daha hızlı hash
- Scaling bloom — büyük set'ler için

**Öncelik:** Orta. Mevcut bloom çalışıyor, optimizasyon sonra.

---

### 5.6 DNSBL Desteği

**Mevcut Durum:** DNS-based blackhole list sorgulama yok.

**Potansiyel:**
- `jehiah/blacklist` DNSBL sorguluyor
- Spamhaus ZEN, SURBL gibi DNSBL'ler
- DNS sorgusu → çok hızlı, rate limit yok

**Öncelik:** Düşük.

---

## 6. Entegrasyon Roadmap'i

### Phase 1 (Öncelikli — Şimdi Başla)

| Sıra | Kaynak | Ne Gerekli? | Tahmini |
|:----:|--------|-------------|:-------:|
| 1 | **ThreatFox** provider | URLhaus'u kopyala, field'ları değiştir | 1 saat |
| 2 | **BlockList.de** provider | HTTP GET + line parse | 1 saat |
| 3 | **cidranger** → IP altyapısı | `go get`, ranger wrapper, `/v2/check?ip=` | 1 gün |

**Phase 1 Sonrası Kapsam:**
- 4 → 6 provider
- IP sorgulama aktif
- 30K+ BlockList.de IP'si veritabanında

---

### Phase 2 (Kısa Vade)

| Sıra | Kaynak | Ne Gerekli? | Tahmini |
|:----:|--------|-------------|:-------:|
| 4 | **Spamhaus DROP** | CIDR parser (Phase 1'den sonra hazır) | 2 saat |
| 5 | **AlienVault OTX** | REST API client, pulse parser | 1 gün |
| 6 | **Phishing.Database** | CSV parser, diff update | 2 saat |
| 7 | **AbuseIPDB** online | PhishTank pattern, API client | 1 gün |

**Phase 2 Sonrası Kapsam:**
- 6 → 10 provider
- Online reputation aktif
- IP + URL + domain + phishing kapsamı 3x

---

### Phase 3 (Opsiyonel)

| Sıra | Kaynak | Ne Gerekli? |
|:----:|--------|-------------|
| 8 | Pi-hole / AdGuard domain list | Hosts/AdBlock format parser |
| 9 | VirusTotal online scan | Community API client |
| 10 | urlscan.io scan | Premium API gerektirir |
| 11 | IPQualityScore | Tam ücretli |

---

## 7. Özet Tablo

| # | Kaynak | Tür | Çekme | Ücret | Efor | Öncelik |
|:-:|--------|:---:|:----:|:----:|:----:|:-------:|
| 1 | **ThreatFox** | Feed | JSON API | 🆓 | 🟢 1s | #1 |
| 2 | **BlockList.de** | Feed | HTTP text | 🆓 | 🟢 1s | #1 |
| 3 | **cidranger** (IP altyapısı) | Lib | Go mod | 🆓 | 🟡 1g | #1 |
| 4 | **Spamhaus DROP** | Feed | HTTP text | 🆓 | 🟢 2s | #2 |
| 5 | **AlienVault OTX** | Feed | REST API | 🆓 | 🟡 1g | #2 |
| 6 | **Phishing.Database** | Feed | GitHub CSV | 🆓 | 🟢 2s | #2 |
| 7 | **AbuseIPDB** | API | REST API | 🆓(1000/gün) | 🟡 1g | #2 |
| 8 | Pi-hole / AdGuard | Feed | HTTP | 🆓 | 🟢 2s | #3 |
| 9 | VirusTotal | API | REST API | 💰 | 🟠 2g | #3 |
| 10 | urlscan.io | API | REST API | 💰 | 🟡 1g | #3 |
| 11 | IPQualityScore | API | REST API | 💰 | 🟢 1s | #4 |
| 12 | MaxMind | DB | MMDB | 🆓/💰 | 🟡 1g | #4 |

**Kısaltmalar:**
- 🆓 = tamamen ücretsiz / ücretsiz katman yeterli
- 💰 = ücretli veya ücretsiz katman çok kısıtlı
- s = saat, g = gün
- #1 = hemen, #2 = yakın, #3 = orta, #4 = düşük öncelik

---

> *"Rakipleri içimize alalım güçlenelim."*
>
> Phase 1 (ThreatFox + BlockList.de + CIDR) ile Blacked sadece URL/domain değil, **IP blacklist** de yapabilen bir araç haline gelir. Phase 2'de AbuseIPDB + OTX ile **online reputation** ve **geniş threat intelligence** kapsamı eklenir. Toplamda 10+ provider ile Blacked, self-hosted URL/IP blacklist alanında en kapsamlı açık kaynak araçlardan biri olur.
