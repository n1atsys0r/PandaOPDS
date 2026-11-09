# Isolated Go parity spike

This is an offline, review-only experiment. The PandaOPDS application remains Python; this directory contains no production integration and changes no root configuration, deployment files, or FolderOPDS files. It does not make any decision about merging FolderOPDS.

The slice is deliberately limited to one public Latest page in the supported extended-list HTML layout, its OPDS 2.0 publication, that publication's detail page, and its first image. The Go code uses `golang.org/x/net/html` for HTML parsing. It preserves the compound `next` cursor verbatim, reads the detail image count and first `/s/` link, parses the first image page, and maps the 509 image placeholder to HTTP 429 with `Retry-After`.

The fixture set is synthetic and shared by the Go server and Python comparison harness. `compare.py` invokes the actual Python FastAPI OPDS 2.0 and stream routers through ASGI, without binding network ports. Its fixture service adapter supplies `parse_list_page`, `parse_detail_page`, and `parse_image_page` results from those shared files and returns the shared JPEG bytes. It does not exercise Python's EH HTTP client, throttling, cache, or the full upstream request chain. Go handlers likewise use an in-process HTTP transport that replays the single-image HTML and JPEG fixtures. No E-Hentai traffic or credentials are used.

Unsupported areas include other list layouts, search, other pages, general 509/error-page variants, authentication, caches, throttling, archive behavior, and production deployment. The Go detail document includes only the first reading-order item. This is a migration spike, not a compatible replacement for the Python service.

## Reproduce

From the repository root, with the repository `.venv` available:

```sh
cd experiments/go-parity
GOPROXY=off GOMODCACHE=/tmp/pandaopds-parity-go-mod GOCACHE=/tmp/pandaopds-parity-go-build go test ./...
cd ../..
PYTHONPATH=. GOPROXY=off GOMODCACHE=/tmp/pandaopds-parity-go-mod GOCACHE=/tmp/pandaopds-parity-go-build .venv/bin/python experiments/go-parity/compare.py
```

The module pins `golang.org/x/net` in `go.mod`/`go.sum`. If the temporary module cache is empty, run `GOMODCACHE=/tmp/pandaopds-parity-go-mod go mod download` from `experiments/go-parity` first. This one-time download needs network access. The replay checks run offline afterward.

## Optional interactive server

From the repository root:

```sh
cd experiments/go-parity
GOMODCACHE=/tmp/pandaopds-parity-go-mod GOCACHE=/tmp/pandaopds-parity-go-build go run .
```

It binds only to `127.0.0.1:8088`. Open the Latest feed at [http://127.0.0.1:8088/opds/v2.0/gallery](http://127.0.0.1:8088/opds/v2.0/gallery). Follow the publication self link, then its first reading-order link. Stop the server with Ctrl-C.
