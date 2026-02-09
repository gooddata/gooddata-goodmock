# GoodMock

A lightweight, high-performance [WireMock](https://wiremock.org/)-compatible HTTP mock server written in Go, powered by [fasthttp](https://github.com/valyala/fasthttp). GoodMock supports **replay only** — it serves pre-defined stub responses from WireMock mapping files but does not record traffic.

## Features

- WireMock-compatible mapping format and admin API (replay only, no recording)
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
./goodmock -port 8080
```

## Usage

### Command-Line Flags

| Flag    | Default | Description          |
|---------|---------|----------------------|
| `-port` | `8080`  | Port to listen on    |

### Environment Variables

| Variable       | Default            | Description                                        |
|----------------|--------------------|----------------------------------------------------|
| `PROXY_HOST`   | `http://localhost` | Proxy host used for cookie domain transformations  |
| `MAPPINGS_DIR` | _(unset)_          | Directory of JSON mapping files to load on startup |

### Loading Mappings on Startup

Set `MAPPINGS_DIR` to a directory containing WireMock-format JSON files:

```bash
MAPPINGS_DIR=./mappings ./goodmock
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

| Method   | Endpoint                       | Description                  |
|----------|--------------------------------|------------------------------|
| `GET`    | `/__admin`                     | Health check                 |
| `GET`    | `/__admin/health`              | Health check                 |
| `GET`    | `/__admin/mappings`            | List all loaded mappings     |
| `POST`   | `/__admin/mappings`            | Add a single mapping         |
| `DELETE` | `/__admin/mappings`            | Delete all mappings          |
| `POST`   | `/__admin/mappings/import`     | Import a batch of mappings   |
| `POST`   | `/__admin/mappings/reset`      | Reset all mappings           |
| `POST`   | `/__admin/reset`               | Reset all mappings           |
| `POST`   | `/__admin/settings`            | Acknowledge settings (no-op) |
| `POST`   | `/__admin/scenarios/reset`     | Reset scenarios (no-op)      |
| `DELETE` | `/__admin/requests`            | Clear request log (no-op)    |
| `POST`   | `/__admin/recordings/snapshot` | Recording snapshot (stub)    |

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

## License

See [LICENSE](LICENSE) for details.
