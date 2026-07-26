# Silo Local Metadata

Silo metadata provider for local Jellyfin-compatible `.nfo` files and sidecar
artwork stored beside media files.

The plugin supports movie and series metadata plus local poster, backdrop, logo,
and still images. Folder-level artwork names include `poster.png`, `folder.jpg`,
`fanart.jpg`, `backdrop.png`, `logo.png`, and `thumb.jpg`.

Silo's built-in `NFO Files` provider currently reads core NFO metadata but does
not discover or serve local sidecar artwork. Keep this plugin enabled in
libraries that rely on local artwork until equivalent built-in support is
available and the library has been migrated.
