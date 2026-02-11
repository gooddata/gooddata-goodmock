# GoodMock

A lightweight, high-performance mock server written in Go, powered by [fasthttp](https://github.com/valyala/fasthttp). GoodMock supports **replay**, **record**, and **proxy** modes — it can serve pre-recorded stub responses, proxy traffic to an upstream backend while recording mapping files, or act as a pure pass-through proxy.

## Features

- WireMock-compatible admin API (`/__admin` endpoints)
- Request matching by URL, URL path, URL pattern (regex), query parameters, headers, and JSON body
- Runtime mapping management via `/__admin` endpoints
- Load mappings from JSON files on startup
- Detailed mismatch logging (WireMock-style diagnostics)
- Minimal Docker image (built from `scratch`)

## Installation

### Docker

```bash
docker build -t goodmock .
docker run -p 8080:8080 goodmock
```

### Building from Source

Prerequisites:
- Go 1.25.6 or later

```bash
go build -o goodmock .
./goodmock replay
```

## Usage

```
goodmock <mode>
```

### Modes

| Mode     | Description                                                 |
|----------|-------------------------------------------------------------|
| `replay` | Serve pre-recorded stub responses (default)                 |
| `record` | Proxy to upstream and record exchanges as WireMock mappings |
| `proxy`  | Proxy to upstream without recording (pure pass-through)     |

### Environment Variables

| Variable                  | Default            | Modes  | Description                                                                                           |
|---------------------------|--------------------|--------|-------------------------------------------------------------------------------------------------------|
| `PORT`                    | `8080`             | all    | Port to listen on                                                                                     |
| `PROXY_HOST`              | `http://localhost` | all    | Upstream host (record: proxy target; replay: header rewriting)                                        |
| `REFERER_PATH`            | `/`                | all    | App-specific path appended to `PROXY_HOST` for Referer header                                         |
| `MAPPINGS_DIR`            | _(unset)_          | replay | Directory of JSON mapping files to load on startup                                                    |
| `VERBOSE`                 | _(unset)_          | all    | Log all request/response traffic (any value enables)                                                  |
| `JSON_CONTENT_TYPES`      | _(unset)_          | record | Additional Content-Types to store as structured JSON (see below)                                      |
| `PRESERVE_JSON_KEY_ORDER` | _(unset)_          | record | Preserve original key order in JSON request and response bodies (any value enables)                   |
| `SORT_ARRAY_MEMBERS`      | _(unset)_          | record | Recursively sort JSON array elements by stringified value for deterministic diffs (any value enables) |

### Loading Mappings on Startup

Set `MAPPINGS_DIR` to a directory containing WireMock-format JSON files:

```bash
MAPPINGS_DIR=./mappings ./goodmock replay
```

Each file should contain a `mappings` array:

```json
{
  "mappings": [
    {
      "request": {
        "method": "GET",
        "urlPath": "/api/example"
      },
      "response": {
        "status": 200,
        "body": "{\"message\": \"hello\"}",
        "headers": {
          "Content-Type": "application/json"
        }
      }
    }
  ]
}
```

## Admin API

GoodMock exposes a subset of the WireMock admin API under `/__admin`:

| Method   | Endpoint                       | Description                            |
|----------|--------------------------------|----------------------------------------|
| `GET`    | `/__admin`                     | Health check                           |
| `GET`    | `/__admin/health`              | Health check                           |
| `GET`    | `/__admin/mappings`            | List all loaded mappings               |
| `POST`   | `/__admin/mappings`            | Add a single mapping                   |
| `DELETE` | `/__admin/mappings`            | Delete all mappings                    |
| `POST`   | `/__admin/mappings/import`     | Import a batch of mappings             |
| `POST`   | `/__admin/mappings/reset`      | Reset all mappings                     |
| `POST`   | `/__admin/reset`               | Reset all mappings                     |
| `POST`   | `/__admin/settings`            | Acknowledge settings (no-op)           |
| `POST`   | `/__admin/scenarios/reset`     | Reset scenarios (no-op)                |
| `DELETE` | `/__admin/requests`            | Clear request log / recordings         |
| `POST`   | `/__admin/recordings/snapshot` | Export recorded mappings (record mode) |

### Adding a Mapping at Runtime

```bash
curl -X POST http://localhost:8080/__admin/mappings \
  -H "Content-Type: application/json" \
  -d '{
    "request": {
      "method": "GET",
      "urlPath": "/api/users"
    },
    "response": {
      "status": 200,
      "body": "[{\"id\": 1, \"name\": \"Alice\"}]",
      "headers": {
        "Content-Type": "application/json"
      }
    }
  }'
```

## Record Mode

In record mode, GoodMock proxies all requests to the upstream backend (`PROXY_HOST`) and captures request/response pairs. Recorded exchanges can be exported as WireMock-compatible mapping files via the snapshot API.

```bash
PROXY_HOST=https://my-backend.example.com ./goodmock record
```

Stubs added via `/__admin/mappings` take priority over proxying (e.g. for mocking log endpoints).

### Exporting Recordings

```bash
curl -X POST http://localhost:8080/__admin/recordings/snapshot \
  -H "Content-Type: application/json" \
  -d '{"persist": false, "repeatsAsScenarios": false}'
```

The snapshot endpoint supports:
- `filters.urlPattern` — regex to filter which recordings to include
- `repeatsAsScenarios` — when `true`, creates scenario-based mappings for repeated URLs
- `persist` — accepted but ignored (mappings are always returned in the response)

## Proxy Mode

In proxy mode, GoodMock forwards all requests to the upstream backend (`PROXY_HOST`) and returns responses to the client — without recording any exchanges. The same header transformations and response filtering (gzip decompression, `X-GDC*`/`Date` stripping) apply as in record mode.

```bash
PROXY_HOST=https://my-backend.example.com ./goodmock proxy
```

This is useful for local development when you want requests routed through GoodMock (with header rewriting) but don't need to capture mappings.

## Structured JSON Response Bodies

In record mode, `application/json` response bodies are always stored as structured JSON in the `jsonBody` field instead of escaped strings in the `body` field. This makes mapping files diffable and human-readable.

`JSON_CONTENT_TYPES` adds additional Content-Types to this behavior:

```bash
JSON_CONTENT_TYPES="application/vnd.gooddata.api+json,application/vnd.api+json" \
  PROXY_HOST=https://my-backend.example.com ./goodmock record
```

This produces mapping files with structured, diffable response bodies:

```json
{
  "response": {
    "status": 200,
    "jsonBody": {
      "data": [1, 2, 3],
      "meta": {"total": 3}
    }
  }
}
```

Instead of:

```json
{
  "response": {
    "status": 200,
    "body": "{\"data\":[1,2,3],\"meta\":{\"total\":3}}"
  }
}
```

Replay mode serves both formats — `jsonBody` is marshaled back to JSON on the fly, `body` is served as-is.

## Request Header Rewriting

GoodMock rewrites incoming request headers before stub matching, equivalent to WireMock's `RequestHeadersTransformer` extension. This ensures requests from the browser (pointing at localhost) match headers recorded against the original proxy host.

| Header            | Rewritten to                      |
|-------------------|-----------------------------------|
| `Origin`          | `PROXY_HOST`                      |
| `Referer`         | `PROXY_HOST` + `REFERER_PATH`     |
| `Accept-Encoding` | `gzip`                            |

Per-app `REFERER_PATH` values:

| App                      | `REFERER_PATH` |
|--------------------------|----------------|
| home-ui                  | `/`            |
| gdc-analytical-designer  | `/analyze/`    |
| gdc-dashboards           | `/dashboards/` |
| gdc-meditor              | `/metrics/`    |
| gdc-msf-modeler          | `/analyze/`    |

## Request Matching

Requests are matched against loaded mappings using the following criteria:

| Field              | Description                                          |
|--------------------|------------------------------------------------------|
| `method`           | HTTP method (`GET`, `POST`, etc., or `ANY`)          |
| `url`              | Exact match on full URI (path + query string)        |
| `urlPath`          | Exact match on path only                             |
| `urlPattern`       | Regex match on full URI                              |
| `queryParameters`  | Match query parameters (`equalTo`, `hasExactly`)     |
| `headers`          | Match headers (`equalTo`, `contains`)                |
| `bodyPatterns`     | Match JSON body (`equalToJson`)                      |

When no mapping matches, GoodMock returns a `404` with a diagnostic log showing the closest stub and where the mismatch occurred.

## WireMock Compatibility

GoodMock's **admin API** (`/__admin` endpoints) is WireMock-compatible — tools like Cypress WireMock integrations work without changes.

Mapping files **produced by record mode** are not WireMock-compatible as of v0.5.0:
- `application/json` response bodies are stored as structured `jsonBody` objects instead of escaped `body` strings. `JSON_CONTENT_TYPES` adds additional Content-Types to this behavior.
- Mappings omit `id` and `uuid` fields (WireMock generates random UUIDs that cause spurious diffs)

**Replay mode is fully backwards-compatible** — old WireMock-format mapping files (with `body` strings, `id`/`uuid` fields, etc.) still load and work without any changes.

## Migrating from WireMock

### Step 1: Switch replay to GoodMock

Replace the WireMock image with GoodMock in your docker-compose for replay mode. Your existing WireMock mapping files will work as-is — no re-recording needed.

```yaml
backend-mock:
    image: gooddata/gooddata-goodmock:0.5.0
    environment:
        - REFERER_PATH=/your-app-path/
```

Run your test suite. If anything breaks, [open an issue](https://github.com/gooddata/gooddata-goodmock/issues).

### Step 2: Switch record to GoodMock

Once replay is stable, replace WireMock for recording as well:

```yaml
backend-mock:
    image: gooddata/gooddata-goodmock:0.5.0
    command: ["record"]
    environment:
        - PROXY_HOST=${HOST}
        - JSON_CONTENT_TYPES=application/vnd.gooddata.api+json
```

Re-record your test mappings, then verify they replay correctly. The new mapping files will use GoodMock's format (`jsonBody`, no `id`/`uuid`) — diffs should be significantly cleaner going forward. If you hit any issues, [open an issue](https://github.com/gooddata/gooddata-goodmock/issues).

## License

See [LICENSE](LICENSE) for details.
