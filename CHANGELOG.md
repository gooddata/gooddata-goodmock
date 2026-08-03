# Changelog
All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.12.0] - 2026-08-03
### Added
- `PROXY_DIAL_TIMEOUT` sets the TCP connect timeout for upstream requests (record and proxy modes), default `30s`
- `PROXY_DIAL_ATTEMPTS` caps the total number of TCP dial attempts when the connection cannot be established at all, initial dial included, default `3`; set to `1` to disable retrying

### Changed
- Dial timeout default raised from fasthttp's implicit 3s to 30s. Read and write timeouts remain unset, so long-running upstream responses are unaffected.

### Fixed
- Upstream requests no longer fail after 3s when the upstream is briefly unreachable. The HTTP client was constructed as `&fasthttp.Client{}` with no `Dial`, so it fell back to `fasthttp.DefaultDialTimeout` (3s) with no retry — shorter than a rolling load-balancer or ingress update, during which SYNs go unanswered. Callers saw `502 {"error": "proxy error: error when dialing <addr>: dialing to the given TCP address timed out"}` while the upstream was healthy. The dial timeout is now explicit and a failed dial is retried.

## [0.11.0] - 2026-04-02
### Changed
- `X-GDC-TRACE-ID` response header is now preserved in proxy and record modes (previously stripped along with all `X-GDC*` headers)
- In record mode, `X-GDC-TRACE-ID` is only forwarded to the client — it is still excluded from saved mappings to keep recordings clean

## [0.10.0] - 2026-03-18
### Changed
- Moved replay mode into its own `internal/replay` package, matching the pattern of `record` and `pureproxy`
- Each mode now has its own admin handler (`handleReplayAdmin`, `handleRecordAdmin`) — admin endpoints are scoped to the mode where they make sense instead of sharing a single catch-all handler
- Mapping-related admin endpoints (`/__admin/mappings`, `/__admin/mappings/import`, `/__admin/mappings/reset`, `/__admin/reset`) are now replay-mode only — record and proxy modes no longer silently accept mapping operations that have no effect
- `/__admin/recordings/snapshot` is now record-mode only — replay and proxy modes return 404 instead of an empty response
- `/__admin/scenarios/reset` now returns 501 (was a silent no-op returning 200)
- `/__admin/settings` now returns 501 (was a silent no-op returning 200)
- `DELETE /__admin/requests` now returns 501 in replay/proxy modes (record mode still clears exchanges)
- Record mode `/__admin/reset` no longer calls `ClearMappings` (mappings are never loaded in record mode)

## [0.9.0] - 2026-03-15
### Added
- `install.sh` script for downloading and installing prebuilt binaries with checksum verification

## [0.8.0] - 2026-03-15
### Added
- Cross-platform binary releases attached to GitHub Releases (linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64, windows/arm64) with SHA-256 checksums

### Changed
- Binaries are now built with `-ldflags="-s -w"` to strip debug symbols and reduce binary size

## [0.7.0] - 2026-03-15
### Added
- `--version` / `-v` flag to print the embedded version and exit
- Version is now embedded in the binary at compile time via `//go:embed VERSION`

## [0.6.0] - 2026-03-10
### Added
- `BINARY_CONTENT_TYPES` environment variable (record + replay) — comma-separated list of Content-Types whose response bodies should be stored as base64-encoded strings in the `body` field. In replay mode, responses with matching Content-Types are automatically base64-decoded before serving.

## [0.5.1] - 2026-02-20
### Changed
- Increased default maximum request body size to 16MB

## [0.5.0] - 2026-02-11
### Added
- `application/json` response bodies are now always stored as structured JSON (`jsonBody`) instead of escaped strings, improving diffability of mapping files
- `JSON_CONTENT_TYPES` environment variable (record mode) — comma-separated list of additional Content-Types to also store as `jsonBody` (e.g. `application/vnd.gooddata.api+json`)
- Replay mode supports serving both `body` (string) and `jsonBody` (structured) response formats
- `PRESERVE_JSON_KEY_ORDER` environment variable (record mode) — when set, preserves original key ordering in both JSON request bodies (`equalToJson`) and response bodies (`jsonBody`) instead of sorting alphabetically
- `SORT_ARRAY_MEMBERS` environment variable (record mode) — when set, recursively sorts JSON array elements by their stringified value (bottom-up) in both request and response bodies, eliminating diffs caused by non-deterministic array ordering from upstream
- Recorded mappings are now sorted deterministically by name (with method + URL + query params + body as tiebreaker for duplicate names), eliminating spurious diffs caused by request-arrival ordering

### Changed
- **Breaking (output only):** Mapping files produced by record mode are no longer WireMock-compatible as of v0.5.0 — `application/json` responses use `jsonBody` (structured JSON) instead of `body` (escaped strings), and mappings omit `id`/`uuid` fields. Replay mode remains fully backwards-compatible: old WireMock-format mapping files (with `body` strings and `id`/`uuid` fields) still load and work without changes. The admin API also remains WireMock-compatible.
- **Breaking (key order):** JSON keys in both request bodies (`equalToJson`) and response bodies (`jsonBody`) are now sorted alphabetically by default for deterministic diffs. If your consumers rely on original key ordering from the upstream server, set `PRESERVE_JSON_KEY_ORDER=true` to restore the previous behaviour.
- `VERBOSE` environment variable now accepts any non-empty value (previously required `true`, `1`, or `yes`)

### Removed
- Removed `id` and `uuid` fields from recorded mappings — they were random UUIDs that caused spurious diffs on every re-record and were never used for matching or lookup

## [0.4.0] - 2026-02-11
### Added
- Proxy mode (`goodmock proxy`) — forwards all traffic to upstream without recording, applying the same header transformations and response filtering as record mode

## [0.3.2] - 2026-02-11
### Changed
- Split monolithic `main` package into `internal/` sub-packages: `types`, `server`, `record`, `matching`, `logging`, `proxy`, `common`
- Exported Server struct fields (`Mappings`, `ProxyHost`, `RefererPath`, `Verbose`, `Mu`) for cross-package access
- Exported public API functions (`HandleRequest`, `HandleAdmin`, `LoadMappings`, `ClearMappings`, `TransformRequestHeaders`, `LogVerboseRequest`, `MatchRequest`, `LogMismatch`, `ProxyRequest`, `RunRecord`)
- Moved shared helpers (`GetPort`, `IsVerbose`) into `internal/common`

## [0.3.1] - 2026-02-11
### Changed
- Refactored codebase from OOP style to functional style — all Server and RecordServer methods converted to free functions
- Pure functions (applyResponseHeaders, evaluateMapping, logMismatch, logVerboseRequest, transformRequestHeaders) no longer take a server receiver
- Replaced RecordServer struct embedding with explicit `server *Server` field
- Request handlers use closures instead of method values

## [0.3.0] - 2026-02-10
### Added
- Record mode (`goodmock record`) — proxies to upstream and captures request/response pairs
- `/__admin/recordings/snapshot` endpoint with URL pattern filtering and scenario support
- Automatic gzip decompression for recorded response bodies
- Scenario-based mappings for repeated URLs (`repeatsAsScenarios`)
- `VERBOSE` environment variable for full request/response traffic logging

## [0.2.1] - 2026-02-10
### Changed
- Added mode as primary CLI arg (`goodmock replay`), defaults to `replay`
- Replaced `-port` flag with `PORT` environment variable (default: 8080)

## [0.2.0] - 2026-02-10
### Added
- Request header rewriting (Origin, Referer, Accept-Encoding) to match recorded stubs
- `REFERER_PATH` environment variable for app-specific Referer header path

### Fixed
- URL path matching now preserves percent-encoding (e.g. `%3A`) by using raw request URI instead of fasthttp's decoded path

## [0.1.1] - 2026-02-10
### Changed
- Refactor and minor improvements

## [0.1.0] - 2026-02-09
### Added
- Initial release

[0.12.0]: https://github.com/gooddata/gooddata-goodmock/compare/v0.11.0...v0.12.0
[0.11.0]: https://github.com/gooddata/gooddata-goodmock/compare/v0.10.0...v0.11.0
[0.10.0]: https://github.com/gooddata/gooddata-goodmock/compare/v0.9.0...v0.10.0
[0.9.0]: https://github.com/gooddata/gooddata-goodmock/compare/v0.8.0...v0.9.0
[0.8.0]: https://github.com/gooddata/gooddata-goodmock/compare/v0.7.0...v0.8.0
[0.7.0]: https://github.com/gooddata/gooddata-goodmock/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/gooddata/gooddata-goodmock/compare/v0.5.1...v0.6.0
[0.5.1]: https://github.com/gooddata/gooddata-goodmock/compare/v0.5.0...v0.5.1
[0.5.0]: https://github.com/gooddata/gooddata-goodmock/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/gooddata/gooddata-goodmock/compare/v0.3.2...v0.4.0
[0.3.2]: https://github.com/gooddata/gooddata-goodmock/compare/v0.3.1...v0.3.2
[0.3.1]: https://github.com/gooddata/gooddata-goodmock/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/gooddata/gooddata-goodmock/compare/v0.2.1...v0.3.0
[0.2.1]: https://github.com/gooddata/gooddata-goodmock/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/gooddata/gooddata-goodmock/compare/v0.1.1...v0.2.0
[0.1.1]: https://github.com/gooddata/gooddata-goodmock/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/gooddata/gooddata-goodmock/releases/tag/v0.1.0
