# Core NFO Artwork Add-on Plan

> Update (2026-08-24): configured-root indexing was removed. Silo supplies the
> exact media path during metadata refresh; Local Artwork performs adjacent
> discovery only, and its resolver validates artwork against a sibling media
> file. References below to configured roots or fallback indexing describe the
> superseded v0.2.0 implementation.

## Outcome

Convert Silo Local Metadata into **Local Artwork**, a compatibility add-on for
Silo's built-in NFO provider rather than a competing NFO parser.

Silo core will own NFO parsing and metadata. The plugin will retain the minimum
metadata-provider handshake required by the current plugin API, discover extra
local artwork conventions, and resolve artwork on installations that do not use
S3.

This work will not change the Silo plugin SDK or protobuf API.

## Target Responsibilities

### Silo core NFO provider

- Parse `.nfo` files.
- Supply titles, summaries, dates, ratings, people, provider IDs, seasons, and
  episodes.
- Validate NFO type and choose among NFO candidates.
- Remain the authoritative local metadata source.

### Local Metadata plugin

- Discover exact local artwork names already supported by the plugin.
- Discover deterministic artwork variants such as `poster-*.png`.
- Return and resolve canonical `local-artwork://` image paths without S3.
- Temporarily resolve previously stored `local-metadata://` paths as a
  read-only migration alias.
- Retain a non-authoritative metadata-provider shim solely to establish the
  plugin provider ID that Silo currently requires before `GetImages` is called.
- Optionally use the presence of an NFO as an indexing anchor, but never parse
  or apply its contents.

## Non-goals

- No SDK or protobuf changes.
- No second NFO parser in the plugin.
- No direct serving of arbitrary files outside configured media roots.
- No change to the installed plugin ID or provider-ID derivation during the
  in-place upgrade.
- No requirement for S3.

## Desired Provider Order

1. Built-in `nfo`: authoritative local metadata.
2. `local-metadata`: non-authoritative artwork compatibility.
3. TMDB, TVDB, and other remote providers: fill remaining metadata gaps.

The deployed library configuration must be updated explicitly; changing the
manifest default alone will not reorder existing library provider rows.

## Implementation Phases

### Phase 1: Lock down the compatibility contract

Add tests before refactoring that prove:

- Existing `local-metadata://` paths still resolve during migration.
- Newly discovered images use `local-artwork://` paths.
- Exact artwork names remain preferred over variants.
- Variant selection is deterministic.
- Artwork is found when no NFO exists.
- Malformed NFO content cannot prevent artwork discovery.
- The provider ID for a media path remains byte-for-byte unchanged.
- A process restart followed by a normal metadata refresh can rediscover the
  same artwork.

Record the current deployed provider ordering and take a small sample of stored
`local-metadata://` paths for post-deployment verification.

### Phase 2: Separate artwork discovery from NFO parsing

Refactor `internal/sidecar` so artwork lookup is an independent operation:

- Replace the combined NFO-and-artwork `Lookup` path with an artwork lookup
  result containing only provider ID and images.
- Remove the XML decoder and all plugin-owned metadata field parsing.
- Keep exact-name and variant candidate ordering in one testable function.
- Do not require an NFO file for direct path-based artwork discovery.
- Keep `movie.nfo` directory discovery only as an optional index anchor until
  the fallback index can be safely simplified.

The artwork lookup must return no metadata fields. In particular, it must not
copy titles, years, provider IDs from XML, plots, ratings, or people into Silo.

### Phase 3: Reduce the provider to a non-authoritative handshake

Adapt `provider/provider.go` without changing the SDK:

- `Search` uses Silo's `_filepath` hint when available and returns the stable
  local provider ID only when local artwork is present.
- Search responses are non-authoritative so they cannot stop core NFO or remote
  providers from contributing metadata.
- Any title/year values required by the host search contract are request
  fallbacks only; they are not read from NFO and are not intended as metadata
  overrides.
- `GetMetadata` returns the stable provider identity and artwork association,
  with an otherwise empty metadata item.
- `GetImages` continues using the remembered/indexed provider ID because the
  current host does not provide filesystem paths in plugin image requests.
- `ResolveImage` treats `local-artwork://` as canonical and temporarily accepts
  `local-metadata://` as a read-only migration alias.

Delete the NFO-specific diagnostic fields and replace them with artwork
candidate/match diagnostics.

### Phase 4: Align filesystem safety with core

Before expanding use of the resolver:

- Restrict discovery and resolution to configured metadata/media roots.
- Require a regular, non-empty image file.
- Reject symlink leaves and resolved paths that escape an allowed root.
- Retain the 8 MiB size limit.
- Validate the detected content as an image before returning a data URL.
- Preserve case-insensitive supported extensions: PNG, JPEG, and WebP.

Backward compatibility does not permit resolving an old stored URL outside the
configured roots. Such a path should fail closed and be rediscovered during a
refresh if it belongs to a valid library.

### Phase 5: Update product identity and documentation

Rename the user-facing plugin to **Local Artwork** and update the manifest and
README to describe local artwork compatibility rather than an NFO metadata
provider. Keep the installed plugin ID stable so deployments upgrade in place.
Use `local-artwork` for newly generated image URLs and resolver registration.

Document:

- Built-in NFO must be enabled and ordered before Local Metadata.
- Local Metadata is still needed for no-S3 artwork and extra filename variants.
- Which exact and variant names are supported.
- Which configured roots control indexed discovery and image resolution.

### Phase 6: Release and staged rollout

1. Release a new plugin version without changing the installation identity.
2. Deploy it while leaving the current provider ordering intact.
3. Verify the new `local-artwork://` resolver and the temporary legacy alias
   against sampled existing artwork URLs.
4. Reorder one test library so built-in NFO is first and Local Metadata second.
5. Refresh that library and compare metadata and artwork results.
6. Confirm malformed and missing NFO files do not suppress local artwork.
7. Roll the ordering change out to `Movies - FA`.
8. Monitor logs and missing-image counts before changing other libraries.
9. Refresh or migrate remaining stored legacy URLs, verify none remain, and
   remove the `local-metadata://` alias in a later release.

Rollback requires restoring both the previous plugin binary and provider
ordering. The transition release must retain the legacy resolver alias so a
rollback does not strand artwork written under the old scheme.

## Silo Upstream Work

These are independent of the plugin refactor and require no SDK changes.

### PR 1: Core local poster variants

Add deterministic `poster-*` fallback support to the built-in NFO artwork
scanner:

- Preserve exact-name precedence.
- Accept the same image extensions as core already supports.
- Sort candidates deterministically before choosing one.
- Apply all existing core size, symlink, and library-root checks.
- Add movie, series, and season tests where applicable.

This reduces the compatibility gap while keeping Local Metadata necessary for
no-S3 installations.

### PR 2: Targeted NFO compatibility, only after a corpus audit

Inventory the NFO files used by `Movies - FA`. Add only compatibility fields
that appear in real files and are missing from core. Likely candidates are:

- Legacy `<imdbid>`, `<tmdbid>`, and `<tvdbid>` when modern `<uniqueid>` is
  absent.
- `<sorttitle>`.
- Original-language aliases.
- `VIDEO_TS.nfo` as a movie candidate for DVD layouts.

Keep these as small, separately reviewable changes rather than porting the
plugin's generic XML parser into core.

### Issue: Managed local image cache without S3

Open a design issue, not an implementation PR, for a Silo-managed filesystem
image cache. It should copy validated images into Silo-controlled storage and
serve them through the existing image endpoint. It should not expose raw media
mount paths.

If core eventually provides this, the remaining Local Metadata resolver can be
retired after stored artwork paths have been refreshed or migrated.

## Acceptance Criteria

- Silo core is the only component parsing NFO XML.
- Local Metadata cannot overwrite core NFO metadata.
- `poster.png` wins over every `poster-*` variant.
- Variant fallback is deterministic across restarts.
- Local artwork works without S3.
- Artwork works with a missing or malformed NFO.
- New artwork paths use `local-artwork://`.
- Previously stored valid `local-metadata://` paths resolve throughout the
  migration window.
- Paths outside configured roots and unsafe symlinks are rejected.
- Movies, series, seasons, and episodes retain core NFO behavior.
- The plugin and catalog validation suites pass before release.

## Recommended Work Order

1. Add compatibility and safety tests.
2. Split artwork discovery from XML parsing.
3. Implement the non-authoritative provider shim.
4. Harden path resolution.
5. Update documentation and manifest language.
6. Test provider ordering locally and on one deployed library.
7. Release and roll out.
8. Submit the core `poster-*` PR independently.
9. Audit the real NFO corpus before proposing parser compatibility PRs.
