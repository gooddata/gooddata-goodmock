# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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

### Fixed
- URL path matching now preserves percent-encoding (e.g. `%3A`) by using raw request URI instead of fasthttp's decoded path

### Added
- Request header rewriting (Origin, Referer, Accept-Encoding) to match recorded stubs
- `REFERER_PATH` environment variable for app-specific Referer header path

## [0.1.1] - 2026-02-10

### Changed
- Refactor and minor improvements

## [0.1.0] - 2026-02-09

### Added
- Initial release

[0.3.2]: https://github.com/gooddata/gooddata-goodmock/compare/v0.3.1...v0.3.2
[0.3.1]: https://github.com/gooddata/gooddata-goodmock/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/gooddata/gooddata-goodmock/compare/v0.2.1...v0.3.0
[0.2.1]: https://github.com/gooddata/gooddata-goodmock/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/gooddata/gooddata-goodmock/compare/v0.1.1...v0.2.0
[0.1.1]: https://github.com/gooddata/gooddata-goodmock/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/gooddata/gooddata-goodmock/releases/tag/v0.1.0
