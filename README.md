# rdap for go

A minimal, RFC-faithful **RDAP client for Go (RFC 9082/9083)** with a tiny CLI (`rdapctl`).  
No WHOIS fallback. Optional helpers in the library for DNS provider inference and DNSSEC fields.

---

## Features

- RDAP lookups for **domain**, **nameserver**, **IP network**, **autnum (ASN)**, and **entity**
- Smart `lookup` that auto-detects the query type
- `tree` mode to **flush the reachable RDAP graph** (nodes + edges), with cycle detection
- Output:
  - `--json` (default for single-object cmds) outputs typed JSON
  - text mode (`--json=false`) for human-friendly summaries
- Simple **Makefile** to:
  - **Bootstrap a pinned Go toolchain** locally (no system-wide install needed)
  - Build, test, lint (optional), and install the CLI

---

## Quick start

```bash
# 1) Clone
git clone https://github.com/datum-labs/rdap.git
cd rdap

# 2) Bootstrap a pinned Go toolchain locally (macOS/Linux; no sudo)
make bootstrap

# 3) Build the CLI (outputs ./bin/rdapctl)
make build

# 4) Try it out (uses the local toolchain automatically via Makefile recipes)
./bin/rdapctl domain example.com
./bin/rdapctl tree example.com --max-depth=5 --follow-links

# (Optional) Install to /usr/local/bin (set PREFIX to change)
sudo make install

# Update dependencies / tidy
make deps

# Run tests
make test
```

---

## CLI usage

Common commands:

- Fetch a domain:
  - `rdapctl domain example.com`
- Auto-detect and fetch:
  - `rdapctl lookup ns1.google.com`
- Explore the full graph:
  - `rdapctl tree example.com --max-depth=5 --follow-links`
- Switch to text output:
  - `rdapctl domain example.com --json=false`

Flags you’ll use often:
- `--json` (default true): emit JSON for single-object commands; `tree` emits a graph `{nodes, edges}` in JSON.
- `--walk`: in text mode, do a shallow, one-level expansion of related items.
- `--follow-links`: (for `tree`) traverse RDAP `links[]` where possible.
- `--max-depth`: (for `tree`) bound recursion (default 5).
- `--tld`: hint for entity/lookup resolution (e.g. `--tld com`).

---

## Environment variables

- `RDAPCTL_UA` – override User-Agent (e.g., `datum-rdapctl/1.0`)
- `RDAPCTL_TIMEOUT` – HTTP timeout (e.g., `10s`, `20s`)
- `RDAPCTL_DNS_BOOTSTRAP` – override IANA DNS bootstrap URL
- `RDAPCTL_IP_BOOTSTRAP` – override IANA IP bootstrap URL
- `RDAPCTL_ASN_BOOTSTRAP` – override IANA ASN bootstrap URL

---

## Building

You have two choices:

1) **Use the local toolchain via Makefile (recommended).**  
   This downloads a pinned Go toolchain under `.toolchain/go` and builds to `./bin/rdapctl`. See **Quick start** commands in <block 1>.

2) **Use your system Go:**
   - Go ≥ 1.24:
     - `go build -o bin/rdapctl ./cmd/rdapctl`

---

## Library import

From your Go code:

```go
package main

import (
	"context"
	"fmt"

	"github.com/datum-labs/rdap"
)

func main() {
	c := rdap.New(
		rdap.WithUserAgent("myapp/1.0 (+https://example.com)"),
	)

	ctx := context.Background()

	d, err := c.Domain(ctx, "example.com")
	if err != nil {
		panic(err)
	}
	fmt.Println("Domain handle:", d.Handle)

	ns, err := c.Nameserver(ctx, "ns1.google.com")
	if err != nil {
		panic(err)
	}
	fmt.Println("Nameserver:", ns.LDHName)
}

```

---

## Contributing

- `make bootstrap` (once), then `make build`, `make test`
- Please run `go fmt` and add/adjust tests for changes.

---

## License

AGPL-3.0-only (see `LICENSE`).
