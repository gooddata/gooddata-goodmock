# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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

[0.2.1]: https://github.com/gooddata/gooddata-goodmock/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/gooddata/gooddata-goodmock/compare/v0.1.1...v0.2.0
[0.1.1]: https://github.com/gooddata/gooddata-goodmock/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/gooddata/gooddata-goodmock/releases/tag/v0.1.0
