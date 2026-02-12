# Web crawler

![Go](https://img.shields.io/badge/go-1.25-00ADD8?logo=go&logoColor=white)
![License](https://img.shields.io/badge/license-MIT-green)
[![Actions Status](https://github.com/IDilettant/go-project-316/actions/workflows/hexlet-check.yml/badge.svg)](https://github.com/IDilettant/go-project-316/actions)
[![CI](https://github.com/IDilettant/go-project-316/actions/workflows/ci.yml/badge.svg)](https://github.com/IDilettant/go-project-316/actions/workflows/ci.yml)
[![Quality Gate Status](https://sonarcloud.io/api/project_badges/measure?project=IDilettant_go-project-316&metric=alert_status)](https://sonarcloud.io/summary/new_code?id=IDilettant_go-project-316)
[![Coverage](https://sonarcloud.io/api/project_badges/measure?project=IDilettant_go-project-316&metric=coverage)](https://sonarcloud.io/summary/new_code?id=IDilettant_go-project-316)
[![Code Smells](https://sonarcloud.io/api/project_badges/measure?project=IDilettant_go-project-316&metric=code_smells)](https://sonarcloud.io/summary/new_code?id=IDilettant_go-project-316)
[![Maintainability Rating](https://sonarcloud.io/api/project_badges/measure?project=IDilettant_go-project-316&metric=sqale_rating)](https://sonarcloud.io/summary/new_code?id=IDilettant_go-project-316)

CLI and library for crawling a site and generating a JSON report.

## Getting started

```bash
make build
make run URL=https://example.com
```

If `URL` is missing, `make run` shows CLI help and exits without panic.


## CLI usage

```bash
go run ./cmd/hexlet-go-crawler --help
```

```bash
make run URL=https://example.com
```

You can also run it directly:

```bash
go run ./cmd/hexlet-go-crawler --depth=1 https://example.com
```

Key flags:

- `--depth`: maximum crawl depth from the root URL (inclusive).
- `--timeout`: per-request timeout.
- `--workers`: number of workers.
- `--user-agent`: custom user agent.
- `--delay`: delay duration for crawl speed (for example `200ms`, `1s`).
- `--rps`: requests per second for crawl speed.
- `--retries`: retries after the first failed attempt.

Depth interpretation:

- `depth=1` includes the root page (depth `0`) and its children (depth `1`).

Crawl speed:

- `--delay=200ms` or `--delay=1s`
- `--rps=5`
- If both are set, `--rps` overrides `--delay`.
- CLI maps these values to `crawler.Options.Delay` and `crawler.Options.RPS`.

Rate limiting is global to the process, not per worker.

Retries:

- `--retries=<N>` sets retries (total attempts = `1 + N`).
- CLI maps this value to `crawler.Options.Retries`.

Retryable conditions:

- transport/network errors (before a response is received),
- HTTP `429`,
- HTTP `5xx`.

Non-retryable conditions:

- any other `4xx`,
- invalid request/URL errors,
- context cancellation/deadline.

Retries use non-zero backoff delay (base `100ms`, exponential, capped at `2s`), and the report reflects the result of the last attempt.


## JSON report format

```json
{
  "root_url": "https://example.com",
  "depth": 1,
  "generated_at": "2024-06-01T12:34:56Z",
  "pages": [
    {
      "url": "https://example.com",
      "depth": 0,
      "http_status": 200,
      "status": "ok",
      "error": "",
      "seo": {
        "has_title": true,
        "title": "Example title",
        "has_description": true,
        "description": "Example description",
        "has_h1": true
      },
      "broken_links": [
        {
          "url": "https://example.com/missing",
          "status_code": 404,
          "error": "Not Found"
        }
      ],
      "assets": [
        {
          "url": "https://example.com/static/logo.png",
          "type": "image",
          "status_code": 200,
          "size_bytes": 12345,
          "error": ""
        }
      ],
      "discovered_at": "2024-06-01T12:34:56Z"
    }
  ]
}
```

All keys are required, even if values are empty strings or empty arrays.

`IndentJSON` changes formatting only (spaces/newlines), not report content.

CLI prints JSON as-is with no extra text before or after it, including the trailing newline.

## Report fields

Report keys:
- `root_url`: root URL provided to the crawler.
- `depth`: max crawl depth, with the root URL at depth 0.
- `generated_at`: RFC3339 timestamp when the report was created.
- `pages`: array of crawled pages.

Page keys:
- `url`: page URL.
- `depth`: page depth.
- `http_status`: response status code (0 when no response was received).
- `status`: `ok` or `error`.
- `error`: error description or empty string.
- `seo`: SEO object.
- `broken_links`: array of broken links.
- `assets`: array of assets.
- `discovered_at`: RFC3339 timestamp when the page was discovered.

SEO keys:
- `has_title`, `title`, `has_description`, `description`, `has_h1`.

Broken link keys:
- `url`, `status_code`, `error`.
- Includes only broken links (`4xx`/`5xx` or network errors).
- Uses absolute URLs.
- Unsupported schemes and empty links are ignored.

SEO behavior:
- Missing `title`/`description`/`h1` produces `false` flags and empty strings.
- HTML entities are decoded (for example, `&amp;` -> `&`).

Asset keys:
- `url`, `type`, `status_code`, `size_bytes`, `error`.
- All asset fields are present even on errors.
- If `Content-Length` is missing, size is derived by fallback logic.
- Assets with `status_code >= 400` are included with error text.
- Asset fetches are cached by URL (no duplicate transport calls).
