# Local Artwork for Silo

Local Artwork is a compatibility add-on for Silo's built-in NFO provider. Silo
core parses NFO metadata; this plugin discovers and serves posters, backdrops,
logos, and stills stored beside media files.

It is useful when local artwork uses naming conventions beyond Silo core's
defaults or when the installation does not use S3-compatible object storage.

## Artwork names

The plugin supports PNG, JPEG, and WebP files.

- Media basename: `Movie-poster.png`, `Movie.poster.jpg`,
  `Movie-fanart.webp`, `Movie-logo.png`, `Episode-thumb.jpg`, and equivalent
  backdrop/logo/still suffixes.
- Folder names: `poster`, `folder`, `backdrop`, `fanart`, `logo`, `clearlogo`,
  `thumb`, and `still`.
- Poster variants: the first deterministically sorted valid `poster-*` file,
  after exact-name candidates.

Artwork discovery does not require an NFO. Missing or malformed NFO files do not
affect this plugin because NFO XML is owned exclusively by Silo core.

## Image URLs

New artwork uses `local-artwork://` URLs. Version 0.2.x also resolves legacy
`local-metadata://` URLs during migration so existing libraries can be
refreshed without losing images.

The resolver returns data URLs directly and does not require S3.

## Configuration

Set `SILO_LOCAL_ARTWORK_ROOTS` to a comma- or newline-separated list of allowed
media roots. For the standard Silo container deployment this is normally:

```text
SILO_LOCAL_ARTWORK_ROOTS=/mnt
```

Discovery and resolution fail closed when no valid root is configured. They
reject paths outside the roots, symlink leaves, non-regular files, empty files,
unsupported extensions, non-image content, and files larger than 8 MiB.
`SILO_LOCAL_METADATA_ROOTS` remains a temporary configuration alias for
upgrades.

Optional diagnostics:

```text
SILO_LOCAL_ARTWORK_DEBUG=1
SILO_LOCAL_ARTWORK_DEBUG_LOG=/tmp/silo-local-artwork-debug.log
```

## Provider order

Configure library providers in this order:

1. Built-in `NFO Files` for authoritative local metadata.
2. `Local Artwork` for artwork discovery and resolution.
3. TMDB, TVDB, and other remote providers for remaining metadata.

The installed plugin ID and capability ID intentionally remain
`silo.ramindex.local-metadata` and `local-metadata` so existing installations
and provider assignments upgrade in place. Only the product, repository, and
canonical image scheme have been renamed.

## Development

```bash
go test ./...
make build
```
