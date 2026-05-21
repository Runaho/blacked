# PR #14 — feat/provider-threatfox | Functional Test Results
Date: 2026-05-21
Branch: feat/provider-threatfox → develop
Status: 🟢 GREEN — MERGED into develop

## Bug Fixed During Review
- **blocklist-de cron format**: `.env.toml`'da `0 */15 * * * *` (6 field, Quartz format) → gocron 5-field bekliyor
  - Fix: `0 */15 * * *` (5 field, standard cron)

## Build
- `go build` → ✅ 40.5 MB binary
- `go vet` → ✅
- `go test ./features/providers/threatfox/...` → ✅ ok (0.650s)

## Server
- `./blacked_test serve` → ✅ started on :8088 (not 8080 — port config)
- gocron error: blocklist-de cron fix sonrası çalıştı

## DB State
- Source: `threatfox-online`
- Total entries: **104,556**
  - domain: 46,206
  - ip: 51,294
  - url: 7,056
- Stored response: `data/responses/threatfox-online_response.dat` (2.0 MB, 2026-05-20 23:00)

## API Endpoints Tested
- `GET /api/v1/hit?url=` → full check (bloom + DB + score), 200 + JSON veya 204
- `GET /api/v1/check?url=` → bloom-only, 200 + JSON

## Functional Tests

### True Positives (expect HTTP 200 + blocked=true)
| URL | Result | Source |
|-----|--------|--------|
| constant-io.isothermalmetric.in.net | ✅ blocked=true | threatfox-online |
| 172.245.155.96 | ✅ blocked=true | threatfox-online |
| bkng-updt.com/lnk.7z | ✅ blocked=true | urlhaus-online |
| ext3ghost.feldspargateway.in.net | ✅ blocked=true | threatfox-online |
| 121.36.79.168 | ✅ blocked=true | threatfox-online |
| futureoffoodisnow.com/ | ✅ blocked=true | threatfox-online |

### False Positives (expect HTTP 204 No Content)
| URL | Result |
|-----|--------|
| google.com | ✅ HTTP 204 |
| github.com | ✅ HTTP 204 |
| stackoverflow.com | ✅ HTTP 204 |
| example.com | ✅ HTTP 204 |
| cloudflare.com | ✅ HTTP 204 |
| amazon.com | ✅ HTTP 204 |

### Bloom-Only (check endpoint)
| URL | Result |
|-----|--------|
| constant-io.isothermalmetric.in.net | ✅ likely=true |
| 172.245.155.96 | ✅ likely=true |

## Verdict: ✅ MERGE OK