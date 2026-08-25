# Local Artwork operations

Operator notes for the Local Artwork add-on. The stable installed plugin ID is
`silo.ramindex.local-metadata`.

## Data flow

1. Silo supplies a media path to the compatibility metadata capability.
2. Local Artwork looks only for adjacent image files; it does not read NFO XML.
3. The plugin returns a stable provider ID derived from the cleaned media path.
4. Image requests reuse that ID through the in-memory request cache.
5. The resolver converts validated `local-artwork://` paths to data URLs.

The built-in NFO provider must be ordered before Local Artwork.

## Configuration-free discovery

No media roots are configured. Silo provides the exact media path during
metadata refresh. File-backed items use adjacent artwork candidates. A series
or season directory may use folder artwork when regular media exists within a
bounded two-level subtree. The resolver repeats the corresponding validation
before reading an image.

## Diagnostics

Enable request diagnostics with:

```text
SILO_LOCAL_ARTWORK_DEBUG=1
```

Logs are appended to `/tmp/silo-local-artwork-debug.log`. Override the path with
`SILO_LOCAL_ARTWORK_DEBUG_LOG`.

Legacy debug environment names are accepted during the 0.2.x migration.

## Troubleshooting

### Metadata changed unexpectedly

Confirm built-in `NFO Files` is ordered before Local Artwork. Local Artwork
returns provider identity and image paths only; it does not parse titles, years,
IDs, plots, ratings, or people from NFO.

### Artwork is missing

1. Confirm the image is PNG, JPEG, or WebP and no larger than 8 MiB.
2. Confirm it uses a supported basename, folder name, or `poster-*` variant.
3. Confirm the image is beside the media file and is not a symlink.
4. Refresh metadata so Silo stores a new `local-artwork://` URL.

### Old artwork disappeared

Version 0.2.x registers both `local-artwork` and `local-metadata` resolver
schemes. Confirm the plugin is enabled and the resolver capability is healthy.

## Compatibility invariants

- Do not change the provider-ID hash derivation during the migration.
- Do not remove the `local-metadata://` resolver alias until stored legacy URLs
  have been refreshed or migrated.
- Do not change the installed plugin ID during the repository rename.
