package sidecar

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLookupReadsSameBasenameNFO(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	media := filepath.Join(dir, "Example Movie (2024).mkv")
	writeFile(t, media, "")
	writeFile(t, filepath.Join(dir, "Example Movie (2024).nfo"), `<movie>
  <title>Sidecar Title</title>
  <originaltitle>Original Sidecar Title</originaltitle>
  <sorttitle>Sidecar, Title</sorttitle>
  <plot>A local overview.</plot>
  <tagline>Local tagline.</tagline>
  <year>2024</year>
  <runtime>112 min</runtime>
  <genre>Drama / Mystery</genre>
  <studio>Example Studio</studio>
  <country>US</country>
  <mpaa>PG-13</mpaa>
  <premiered>2024-05-01</premiered>
  <imdbid>tt1234567</imdbid>
  <tmdbid>98765</tmdbid>
  <rating name="imdb"><value>7.4</value></rating>
  <actor><name>Jane Example</name><role>Lead</role><order>2</order></actor>
</movie>`)

	got, err := NewProvider().Lookup(media)
	if err != nil {
		t.Fatalf("Lookup() error = %v", err)
	}
	if got == nil {
		t.Fatal("Lookup() returned nil")
	}
	if got.Item.Title != "Sidecar Title" {
		t.Fatalf("Title = %q", got.Item.Title)
	}
	if got.Item.Year != 2024 {
		t.Fatalf("Year = %d", got.Item.Year)
	}
	if got.Item.RuntimeMinutes != 112 {
		t.Fatalf("RuntimeMinutes = %d", got.Item.RuntimeMinutes)
	}
	if got.Item.ProviderIDs["imdb"] != "tt1234567" || got.Item.ProviderIDs["tmdb"] != "98765" {
		t.Fatalf("ProviderIDs = %#v", got.Item.ProviderIDs)
	}
	if got.Item.Ratings["imdb"] != 7.4 {
		t.Fatalf("Ratings = %#v", got.Item.Ratings)
	}
	if len(got.Item.People) != 1 || got.Item.People[0].Name != "Jane Example" || got.Item.People[0].Character != "Lead" {
		t.Fatalf("People = %#v", got.Item.People)
	}
	if got.Item.People[0].Kind != "Actor" {
		t.Fatalf("People[0].Kind = %q, want Actor", got.Item.People[0].Kind)
	}
}

func TestLookupReadsJellyfinMovieFolderNFO(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	media := filepath.Join(dir, "Movie Folder", "Movie File [WEBDL-1080p].mkv")
	writeFile(t, media, "")
	writeFile(t, filepath.Join(filepath.Dir(media), "movie.nfo"), `<movie>
  <title>Folder NFO Title</title>
  <plot>Read from Jellyfin movie.nfo.</plot>
  <year>2026</year>
</movie>`)

	got, err := NewProvider().Lookup(media, "movie")
	if err != nil {
		t.Fatalf("Lookup() error = %v", err)
	}
	if got == nil {
		t.Fatal("Lookup() returned nil")
	}
	if got.Item.Title != "Folder NFO Title" {
		t.Fatalf("Title = %q", got.Item.Title)
	}
	if got.Item.Year != 2026 {
		t.Fatalf("Year = %d", got.Item.Year)
	}
	if got.Item.Metadata["sidecar_nfo_path"] != filepath.Join(filepath.Dir(media), "movie.nfo") {
		t.Fatalf("sidecar_nfo_path = %#v", got.Item.Metadata["sidecar_nfo_path"])
	}
}

func TestLookupMapsJellyfinPeopleToSiloKinds(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	media := filepath.Join(dir, "People Movie.mkv")
	writeFile(t, media, "")
	writeFile(t, filepath.Join(dir, "People Movie.nfo"), `<movie>
  <title>People Movie</title>
  <director>Mohammad Banki</director>
  <writer>Writer One / Writer Two</writer>
  <actor><name>Hamid Askari</name><order>0</order></actor>
</movie>`)

	got, err := NewProvider().Lookup(media, "movie")
	if err != nil {
		t.Fatalf("Lookup() error = %v", err)
	}
	if got == nil {
		t.Fatal("Lookup() returned nil")
	}

	want := map[string]string{
		"Mohammad Banki": "Director",
		"Writer One":     "Writer",
		"Writer Two":     "Writer",
		"Hamid Askari":   "Actor",
	}
	for _, person := range got.Item.People {
		if want[person.Name] == "" {
			t.Fatalf("unexpected person %#v in %#v", person, got.Item.People)
		}
		if person.Kind != want[person.Name] {
			t.Fatalf("person %q kind = %q, want %q", person.Name, person.Kind, want[person.Name])
		}
		delete(want, person.Name)
	}
	if len(want) > 0 {
		t.Fatalf("missing people %#v from %#v", want, got.Item.People)
	}
}

func TestLookupFindsSameBasenameAndJellyfinFolderImages(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	media := filepath.Join(dir, "Show - S01E02.mkv")
	writeFile(t, media, "")
	writeFile(t, filepath.Join(dir, "Show - S01E02-poster.png"), "png")
	writeFile(t, filepath.Join(dir, "Show - S01E02-fanart.jpg"), "jpg")
	writeFile(t, filepath.Join(dir, "poster.png"), "folder poster")
	writeFile(t, filepath.Join(dir, "folder.jpg"), "folder jpg")
	writeFile(t, filepath.Join(dir, "tvshow.nfo"), "<tvshow><title>Show Title</title></tvshow>")

	got, err := NewProvider().Lookup(media)
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
	writeFile(t, filepath.Join(dir, "poster-fr.jpg"), "french poster")
	writeFile(t, filepath.Join(dir, "poster-en.png"), "english poster")
	writeFile(t, filepath.Join(dir, "poster-.png"), "missing variant name")
	writeFile(t, filepath.Join(dir, "poster-es.gif"), "unsupported format")

	got, err := NewProvider().Lookup(media)
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

func TestLookupReadsJellyfinSeriesAndSeasonNFO(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	seriesDir := filepath.Join(dir, "Example Show")
	seasonDir := filepath.Join(seriesDir, "Season 01")
	writeFile(t, filepath.Join(seriesDir, "tvshow.nfo"), `<tvshow><title>Series Title</title></tvshow>`)
	writeFile(t, filepath.Join(seasonDir, "season.nfo"), `<season><title>Season Title</title></season>`)

	series, err := NewProvider().Lookup(seriesDir, "series")
	if err != nil {
		t.Fatalf("series Lookup() error = %v", err)
	}
	if series == nil || series.Item.Title != "Series Title" {
		t.Fatalf("series Lookup() = %#v", series)
	}

	season, err := NewProvider().Lookup(seasonDir, "season")
	if err != nil {
		t.Fatalf("season Lookup() error = %v", err)
	}
	if season == nil || season.Item.Title != "Season Title" {
		t.Fatalf("season Lookup() = %#v", season)
	}
}

func TestLookupReturnsNilWhenNoSidecarExists(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	media := filepath.Join(dir, "No Metadata.mkv")
	writeFile(t, media, "")

	got, err := NewProvider().Lookup(media)
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
	image := filepath.Join(dir, "Movie-poster.jpg")
	writeFile(t, image, "jpg")

	got, err := NewProvider().ResolveImage(Scheme + image)
	if err != nil {
		t.Fatalf("ResolveImage() error = %v", err)
	}
	want := "data:image/jpeg;base64,anBn"
	if got != want {
		t.Fatalf("ResolveImage() = %q, want %q", got, want)
	}
}

func TestResolveImageUsesDataURLForBareResolverPath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	image := filepath.Join(dir, "Movie-poster.jpg")
	writeFile(t, image, "jpg")

	got, err := NewProvider().ResolveImage(image)
	if err != nil {
		t.Fatalf("ResolveImage() error = %v", err)
	}
	want := "data:image/jpeg;base64,anBn"
	if got != want {
		t.Fatalf("ResolveImage() = %q, want %q", got, want)
	}
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
