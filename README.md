# gopkg

Browse Go packages on pkg.go.dev

`gopkg` is a single pure-Go binary. It speaks to pkggodev over plain
HTTPS, shapes the responses into clean records, and pipes into the rest of your
tools. No API key, nothing to run alongside it.

## Install

```bash
go install github.com/tamnd/pkggodev-cli/cmd/gopkg@latest
```

Or grab a prebuilt binary from the [releases](https://github.com/tamnd/pkggodev-cli/releases), or run
the container image:

```bash
docker run --rm ghcr.io/tamnd/gopkg:latest --help
```

## Usage

```bash
gopkg --help
gopkg version
```

This is a fresh scaffold. The command tree starts with `version`; build out the
real commands in `cli/` on top of the `pkggodev` library package.

## Development

```
cmd/gopkg/   thin main, wires cli.Root into fang
cli/                 the cobra command tree
pkggodev/                the library: HTTP client and data models
docs/                tago documentation site
```

```bash
make build      # ./bin/gopkg
make test       # go test ./...
make vet        # go vet ./...
```

## Releasing

Push a version tag and GitHub Actions runs GoReleaser, which builds the
archives, Linux packages, the multi-arch GHCR image, checksums, SBOMs, and a
cosign signature:

```bash
git tag v0.1.0
git push --tags
```

The Homebrew and Scoop steps self-disable until their tokens exist, so the first
release works with no extra secrets.

## License

Apache-2.0. See [LICENSE](LICENSE).
