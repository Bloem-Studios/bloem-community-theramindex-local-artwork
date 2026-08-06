package sidecar

import (
	"os"
	"path/filepath"
	"testing"
)

func TestArtworkSchemesUseNewCanonicalNameAndKeepLegacyAlias(t *testing.T) {
	t.Parallel()

	if Scheme != "local-artwork://" {
		t.Fatalf("Scheme = %q, want local-artwork://", Scheme)
	}
	if LegacyScheme != "local-metadata://" {
		t.Fatalf("LegacyScheme = %q, want local-metadata://", LegacyScheme)
	}
}

func TestProviderIDRemainsStableAcrossRename(t *testing.T) {
	t.Parallel()

	const mediaPath = "/media/Movies/Example Movie/Example Movie.mkv"
	if got := providerID(mediaPath); got != "228377d51ca9b2de57a64bdf" {
		t.Fatalf("providerID(%q) = %q, want legacy hash", mediaPath, got)
	}
}

func TestLookupFindsSameBasenameAndJellyfinFolderImages(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	media := filepath.Join(dir, "Show - S01E02.mkv")
	writeFile(t, media, "")
	writeFile(t, filepath.Join(dir, "Show - S01E02-poster.png"), validPNG)
	writeFile(t, filepath.Join(dir, "Show - S01E02-fanart.jpg"), validPNG)
	writeFile(t, filepath.Join(dir, "poster.png"), validPNG)
	writeFile(t, filepath.Join(dir, "folder.jpg"), validPNG)
	writeFile(t, filepath.Join(dir, "tvshow.nfo"), "<tvshow><title>Show Title</title></tvshow>")

	got, err := NewProviderWithRoots([]string{dir}).Lookup(media)
	if err != nil {
		t.Fatalf("Lookup() error = %v", err)
	}
	if got == nil {
		t.Fatal("Lookup() returned nil")
	}
	if len(got.Images) != 4 {
		t.Fatalf("Images length = %d, images = %#v", len(got.Images), got.Images)
	}
	byName := map[string]string{}
	for _, img := range got.Images {
		byName[filepath.Base(img.Path)] = img.Kind
	}
	for name, kind := range map[string]string{
		"Show - S01E02-poster.png": "poster",
		"Show - S01E02-fanart.jpg": "backdrop",
		"poster.png":               "poster",
		"folder.jpg":               "poster",
	} {
		if byName[name] != kind {
			t.Fatalf("image %s kind = %q, images = %#v", name, byName[name], got.Images)
		}
	}
}

func TestLookupFindsFolderPosterVariantsInDeterministicOrder(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	media := filepath.Join(dir, "Movie.mkv")
	writeFile(t, media, "")
	writeFile(t, filepath.Join(dir, "poster-fr.jpg"), validPNG)
	writeFile(t, filepath.Join(dir, "poster-en.png"), validPNG)
	writeFile(t, filepath.Join(dir, "poster-.png"), "missing variant name")
	writeFile(t, filepath.Join(dir, "poster-es.gif"), "unsupported format")

	got, err := NewProviderWithRoots([]string{dir}).Lookup(media)
	if err != nil {
		t.Fatalf("Lookup() error = %v", err)
	}
	if got == nil {
		t.Fatal("Lookup() returned nil")
	}

	want := []string{"poster-en.png"}
	if len(got.Images) != len(want) {
		t.Fatalf("Images length = %d, want %d: %#v", len(got.Images), len(want), got.Images)
	}
	for i, image := range got.Images {
		if image.Kind != "poster" {
			t.Fatalf("Images[%d].Kind = %q, want poster", i, image.Kind)
		}
		if name := filepath.Base(image.Path); name != want[i] {
			t.Fatalf("Images[%d] = %q, want %q", i, name, want[i])
		}
	}
}

func TestLookupFindsExactArtworkWithUppercaseExtension(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	media := filepath.Join(dir, "Movie.mkv")
	writeFile(t, media, "")
	writeFile(t, filepath.Join(dir, "poster.PNG"), validPNG)

	got, err := NewProviderWithRoots([]string{dir}).Lookup(media)
	if err != nil {
		t.Fatalf("Lookup() error = %v", err)
	}
	if got == nil || len(got.Images) != 1 || filepath.Base(got.Images[0].Path) != "poster.PNG" {
		t.Fatalf("Lookup() = %#v, want poster.PNG", got)
	}
}

func TestLookupSkipsInvalidPosterVariantAndUsesNextValidCandidate(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	media := filepath.Join(dir, "Movie.mkv")
	writeFile(t, media, "")
	writeFile(t, filepath.Join(dir, "poster-a.png"), "not an image")
	writeFile(t, filepath.Join(dir, "poster-b.png"), validPNG)

	got, err := NewProviderWithRoots([]string{dir}).Lookup(media)
	if err != nil {
		t.Fatalf("Lookup() error = %v", err)
	}
	if got == nil || len(got.Images) != 1 {
		t.Fatalf("Lookup() = %#v, want one valid variant", got)
	}
	if name := filepath.Base(got.Images[0].Path); name != "poster-b.png" {
		t.Fatalf("variant = %q, want poster-b.png", name)
	}
}

func TestLookupFindsArtworkWhenNFOIsMalformed(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	media := filepath.Join(dir, "Movie.mkv")
	writeFile(t, media, "")
	writeFile(t, filepath.Join(dir, "movie.nfo"), "<movie><broken>")
	writeFile(t, filepath.Join(dir, "poster.png"), validPNG)

	got, err := NewProviderWithRoots([]string{dir}).Lookup(media, "movie")
	if err != nil {
		t.Fatalf("Lookup() error = %v", err)
	}
	if got == nil || len(got.Images) != 1 {
		t.Fatalf("Lookup() = %#v, want one artwork result", got)
	}
}

func TestLookupReturnsNilWhenNoSidecarExists(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	media := filepath.Join(dir, "No Metadata.mkv")
	writeFile(t, media, "")

	got, err := NewProviderWithRoots([]string{dir}).Lookup(media)
	if err != nil {
		t.Fatalf("Lookup() error = %v", err)
	}
	if got != nil {
		t.Fatalf("Lookup() = %#v, want nil", got)
	}
}

func TestResolveImageUsesDataURLForExistingSidecarPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	image := filepath.Join(dir, "Movie-poster.png")
	writeFile(t, image, validPNG)

	got, err := NewProviderWithRoots([]string{dir}).ResolveImage(Scheme + image)
	if err != nil {
		t.Fatalf("ResolveImage() error = %v", err)
	}
	want := "data:image/png;base64," + validPNGBase64
	if got != want {
		t.Fatalf("ResolveImage() = %q, want %q", got, want)
	}
}

func TestResolveImageUsesDataURLForBareResolverPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	image := filepath.Join(dir, "Movie-poster.png")
	writeFile(t, image, validPNG)

	got, err := NewProviderWithRoots([]string{dir}).ResolveImage(image)
	if err != nil {
		t.Fatalf("ResolveImage() error = %v", err)
	}
	want := "data:image/png;base64," + validPNGBase64
	if got != want {
		t.Fatalf("ResolveImage() = %q, want %q", got, want)
	}
}

func TestResolveImageSupportsLegacySchemeDuringMigration(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	image := filepath.Join(dir, "poster.png")
	writeFile(t, image, validPNG)

	got, err := NewProviderWithRoots([]string{dir}).ResolveImage(LegacyScheme + image)
	if err != nil {
		t.Fatalf("ResolveImage() error = %v", err)
	}
	if got == "" {
		t.Fatal("ResolveImage() returned an empty URL for legacy scheme")
	}
}

func TestResolveImageRejectsPathOutsideConfiguredRoots(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outside := t.TempDir()
	insideImage := filepath.Join(root, "poster.png")
	outsideImage := filepath.Join(outside, "poster.png")
	writeFile(t, insideImage, validPNG)
	writeFile(t, outsideImage, validPNG)
	p := NewProviderWithRoots([]string{root})

	insideURL, err := p.ResolveImage(Scheme + insideImage)
	if err != nil || insideURL == "" {
		t.Fatalf("ResolveImage(inside) = %q, %v", insideURL, err)
	}
	outsideURL, err := p.ResolveImage(Scheme + outsideImage)
	if err != nil {
		t.Fatalf("ResolveImage(outside) error = %v", err)
	}
	if outsideURL != "" {
		t.Fatalf("ResolveImage(outside) = %q, want empty", outsideURL)
	}
}

func TestResolveImageFailsClosedWhenConfiguredRootIsInvalid(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	image := filepath.Join(dir, "poster.png")
	writeFile(t, image, validPNG)
	p := NewProviderWithRoots([]string{filepath.Join(dir, "missing-root")})

	resolved, err := p.ResolveImage(Scheme + image)
	if err != nil {
		t.Fatalf("ResolveImage() error = %v", err)
	}
	if resolved != "" {
		t.Fatalf("ResolveImage() = %q, want empty", resolved)
	}
}

func TestResolveImageFailsClosedWithoutConfiguredRoots(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	image := filepath.Join(dir, "poster.png")
	writeFile(t, image, validPNG)

	resolved, err := NewProviderWithRoots(nil).ResolveImage(Scheme + image)
	if err != nil {
		t.Fatalf("ResolveImage() error = %v", err)
	}
	if resolved != "" {
		t.Fatalf("ResolveImage() = %q, want empty", resolved)
	}
}

func TestLookupAndResolveRejectSymlinkImageLeaves(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	media := filepath.Join(root, "Movie.mkv")
	target := filepath.Join(root, "real-poster.png")
	linked := filepath.Join(root, "poster.png")
	writeFile(t, media, "")
	writeFile(t, target, validPNG)
	if err := os.Symlink(target, linked); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}
	p := NewProviderWithRoots([]string{root})

	result, err := p.Lookup(media, "movie")
	if err != nil {
		t.Fatalf("Lookup() error = %v", err)
	}
	if result != nil {
		t.Fatalf("Lookup() = %#v, want nil", result)
	}
	resolved, err := p.ResolveImage(Scheme + linked)
	if err != nil {
		t.Fatalf("ResolveImage() error = %v", err)
	}
	if resolved != "" {
		t.Fatalf("ResolveImage() = %q, want empty", resolved)
	}
}

func TestResolveImageRejectsNonImageContent(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	image := filepath.Join(root, "poster.png")
	writeFile(t, image, "not an image")

	resolved, err := NewProviderWithRoots([]string{root}).ResolveImage(Scheme + image)
	if err != nil {
		t.Fatalf("ResolveImage() error = %v", err)
	}
	if resolved != "" {
		t.Fatalf("ResolveImage() = %q, want empty", resolved)
	}
}

const validPNGBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="

var validPNG = string([]byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x04, 0x00, 0x00, 0x00, 0xb5, 0x1c, 0x0c,
	0x02, 0x00, 0x00, 0x00, 0x0b, 0x49, 0x44, 0x41, 0x54, 0x78, 0xda, 0x63, 0x64, 0xf8, 0x0f, 0x00,
	0x01, 0x05, 0x01, 0x01, 0x27, 0x18, 0xe3, 0x66, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44,
	0xae, 0x42, 0x60, 0x82,
})

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
