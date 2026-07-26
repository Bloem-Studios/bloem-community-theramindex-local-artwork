package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
	pluginruntime "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginsdk/runtime"
	"github.com/theramindex/silo-plugin-local-metadata/internal/sidecar"
	"github.com/theramindex/silo-plugin-local-metadata/provider"
	"google.golang.org/grpc"
)

func TestManifestAdvertisesImageResolverCapability(t *testing.T) {
	t.Parallel()

	manifest, err := loadManifest()
	if err != nil {
		t.Fatalf("loadManifest() error = %v", err)
	}

	for _, capability := range manifest.GetCapabilities() {
		if capability.GetType() == "image_resolver.v1" && capability.GetId() == sidecar.CapabilityID {
			return
		}
	}
	t.Fatalf("manifest capabilities missing image_resolver.v1 id %q", sidecar.CapabilityID)
}

func TestRuntimeRegistersImageResolverServer(t *testing.T) {
	t.Parallel()

	rs := &runtimeServer{provider: provider.NewProvider()}
	ms := &metadataServer{runtime: rs}
	server := grpc.NewServer()
	t.Cleanup(server.Stop)

	err := (&pluginruntime.GRPCPlugin{Servers: runtimeServers(rs, ms)}).GRPCServer(nil, server)
	if err != nil {
		t.Fatalf("GRPCServer() error = %v", err)
	}
	if _, ok := server.GetServiceInfo()["silo.plugin.v1.ImageResolver"]; !ok {
		t.Fatalf("ImageResolver service not registered; services=%v", server.GetServiceInfo())
	}
}

func TestMetadataServerSearchReturnsSyntheticLocalCandidate(t *testing.T) {
	t.Parallel()

	ms := &metadataServer{
		runtime: &runtimeServer{provider: provider.NewProvider()},
	}
	resp, err := ms.Search(context.Background(), &pluginv1.SearchMetadataRequest{
		Query:    "Local Movie",
		ItemType: "movie",
		Year:     2025,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	results := resp.GetResults()
	if len(results) != 1 {
		t.Fatalf("Search() results length = %d, want 1", len(results))
	}
	result := results[0]
	if result.GetTitle() != "Local Movie" {
		t.Fatalf("Title = %q", result.GetTitle())
	}
	if result.GetProviderId() == "" {
		t.Fatal("ProviderId is empty")
	}
	providerIDs := result.GetProviderIds().AsMap()
	if got := providerIDs["local"]; got != result.GetProviderId() {
		t.Fatalf("provider_ids[local] = %v, want %q", got, result.GetProviderId())
	}
	if got := providerIDs["local-metadata"]; got != result.GetProviderId() {
		t.Fatalf("provider_ids[local-metadata] = %v, want %q", got, result.GetProviderId())
	}
}

func TestMetadataServerSearchReturnsSyntheticCandidateForEmptyQuery(t *testing.T) {
	t.Parallel()

	ms := &metadataServer{
		runtime: &runtimeServer{provider: provider.NewProvider()},
	}
	resp, err := ms.Search(context.Background(), &pluginv1.SearchMetadataRequest{
		ItemType: "movie",
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	results := resp.GetResults()
	if len(results) != 1 {
		t.Fatalf("Search() results length = %d, want 1", len(results))
	}
	if got := results[0].GetTitle(); got != "Local Metadata" {
		t.Fatalf("Title = %q, want Local Metadata", got)
	}
	if got := results[0].GetYear(); got <= 0 {
		t.Fatalf("Year = %d, want positive fallback year", got)
	}
}

func TestMetadataServerSearchInfersPersianCalendarYear(t *testing.T) {
	t.Parallel()

	ms := &metadataServer{
		runtime: &runtimeServer{provider: provider.NewProvider()},
	}
	resp, err := ms.Search(context.Background(), &pluginv1.SearchMetadataRequest{
		Query:    "فیلم جدید درام عاشق پیشه (محصول سال 1402)",
		ItemType: "movie",
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	results := resp.GetResults()
	if len(results) != 1 {
		t.Fatalf("Search() results length = %d, want 1", len(results))
	}
	if got := results[0].GetYear(); got != 2023 {
		t.Fatalf("Year = %d, want 2023", got)
	}
}

func TestMetadataServerSearchUsesIndexedMovieNFO(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "movies-fa")
	movieDir := filepath.Join(root, "1 2 3")
	media := filepath.Join(movieDir, "1 2 3 [WEBDL-480p 8-bit AVC AAC]-GLWiZ.mp4")
	mustWrite(t, media, "")
	mustWrite(t, filepath.Join(movieDir, "movie.nfo"), `<movie>
  <title>1 2 3</title>
  <originaltitle>1 2 3</originaltitle>
  <sorttitle>1 2 3</sorttitle>
  <thumb aspect="poster">poster.png</thumb>
  <tag>provider:glwiz</tag>
</movie>`)
	mustWrite(t, filepath.Join(movieDir, "poster.png"), "png")
	t.Setenv("SILO_LOCAL_METADATA_ROOTS", root)

	ms := &metadataServer{
		runtime: &runtimeServer{provider: provider.NewProvider()},
	}
	searchResp, err := ms.Search(context.Background(), &pluginv1.SearchMetadataRequest{
		Query:    "1 2 3 [WEBDL-480p 8-bit AVC AAC]-GLWiZ",
		ItemType: "movie",
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	results := searchResp.GetResults()
	if len(results) != 1 {
		t.Fatalf("Search() results length = %d, want 1", len(results))
	}
	result := results[0]
	if got := result.GetTitle(); got != "1 2 3" {
		t.Fatalf("Search result Title = %q, want 1 2 3", got)
	}
	if got := result.GetProviderId(); got == "" {
		t.Fatal("Search result ProviderId is empty")
	}

	metadataResp, err := ms.GetMetadata(context.Background(), &pluginv1.GetMetadataRequest{
		ProviderId: result.GetProviderId(),
		ItemType:   "movie",
	})
	if err != nil {
		t.Fatalf("GetMetadata() error = %v", err)
	}
	item := metadataResp.GetItem()
	if item == nil {
		t.Fatal("GetMetadata().Item is nil")
	}
	if got := item.GetTitle(); got != "1 2 3" {
		t.Fatalf("Metadata Title = %q, want 1 2 3", got)
	}
	if got := item.GetProviderId(); got != result.GetProviderId() {
		t.Fatalf("Metadata ProviderId = %q, want %q", got, result.GetProviderId())
	}
	if got := item.GetPosterPath(); got == "" {
		t.Fatal("Metadata PosterPath is empty")
	}
}

func TestMetadataServerSearchUsesFilePathProviderIDSidecar(t *testing.T) {
	dir := t.TempDir()
	movieDir := filepath.Join(dir, "movies", "Example Movie")
	media := filepath.Join(movieDir, "Example Movie [WEBDL-1080p].mp4")
	mustWrite(t, media, "")
	mustWrite(t, filepath.Join(movieDir, "movie.nfo"), `<movie>
  <title>Example Movie</title>
  <year>2024</year>
</movie>`)
	mustWrite(t, filepath.Join(movieDir, "poster.png"), "png")

	providerIDs, err := stringStruct(map[string]string{"_filepath": media})
	if err != nil {
		t.Fatalf("stringStruct() error = %v", err)
	}
	ms := &metadataServer{
		runtime: &runtimeServer{provider: provider.NewProvider()},
	}
	searchResp, err := ms.Search(context.Background(), &pluginv1.SearchMetadataRequest{
		Query:       "Example Movie [WEBDL-1080p]",
		ItemType:    "movie",
		ProviderIds: providerIDs,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	results := searchResp.GetResults()
	if len(results) != 1 {
		t.Fatalf("Search() results length = %d, want 1", len(results))
	}
	result := results[0]
	if got := result.GetTitle(); got != "Example Movie" {
		t.Fatalf("Search result Title = %q, want Example Movie", got)
	}
	if got := result.GetYear(); got != 2024 {
		t.Fatalf("Search result Year = %d, want 2024", got)
	}
	if got := result.GetProviderId(); got == "" {
		t.Fatal("Search result ProviderId is empty")
	}
	if got := result.GetImageUrl(); got == "" {
		t.Fatal("Search result ImageUrl is empty")
	}
}

func TestMetadataServerGetImagesUsesFilePathProviderIDSidecar(t *testing.T) {
	dir := t.TempDir()
	movieDir := filepath.Join(dir, "movies", "Example Movie")
	media := filepath.Join(movieDir, "Example Movie.mkv")
	mustWrite(t, media, "")
	mustWrite(t, filepath.Join(movieDir, "movie.nfo"), `<movie>
  <title>Example Movie</title>
</movie>`)
	mustWrite(t, filepath.Join(movieDir, "poster.png"), "png")

	ms := &metadataServer{
		runtime: &runtimeServer{provider: provider.NewProvider()},
	}
	providerIDs, err := stringStruct(map[string]string{
		"_filepath":          media,
		sidecar.CapabilityID: "local-sidecar-id",
	})
	if err != nil {
		t.Fatalf("stringStruct() error = %v", err)
	}
	resp, err := ms.GetImages(context.Background(), &pluginv1.GetImagesRequest{
		ProviderId:  "local-sidecar-id",
		ItemType:    "movie",
		ProviderIds: providerIDs,
	})
	if err != nil {
		t.Fatalf("GetImages() error = %v", err)
	}
	images := resp.GetImages()
	if len(images) != 1 {
		t.Fatalf("GetImages() length = %d, want 1", len(images))
	}
	if got := images[0].GetKind(); got != "poster" {
		t.Fatalf("Image kind = %q, want poster", got)
	}
	if got := images[0].GetUrl(); got == "" {
		t.Fatal("Image URL is empty")
	}
}

func TestMetadataServerGetImagesUsesSearchProviderIDCache(t *testing.T) {
	dir := t.TempDir()
	movieDir := filepath.Join(dir, "movies", "Example Movie")
	media := filepath.Join(movieDir, "Example Movie.mkv")
	mustWrite(t, media, "")
	mustWrite(t, filepath.Join(movieDir, "movie.nfo"), `<movie>
  <title>Example Movie</title>
</movie>`)
	mustWrite(t, filepath.Join(movieDir, "poster.png"), "png")

	filePathProviderIDs, err := stringStruct(map[string]string{"_filepath": media})
	if err != nil {
		t.Fatalf("stringStruct() error = %v", err)
	}
	ms := &metadataServer{
		runtime: &runtimeServer{provider: provider.NewProvider()},
	}
	searchResp, err := ms.Search(context.Background(), &pluginv1.SearchMetadataRequest{
		Query:       "Example Movie",
		ItemType:    "movie",
		ProviderIds: filePathProviderIDs,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	results := searchResp.GetResults()
	if len(results) != 1 {
		t.Fatalf("Search() results length = %d, want 1", len(results))
	}

	imageResp, err := ms.GetImages(context.Background(), &pluginv1.GetImagesRequest{
		ProviderId:  results[0].GetProviderId(),
		ItemType:    "movie",
		ProviderIds: results[0].GetProviderIds(),
	})
	if err != nil {
		t.Fatalf("GetImages() error = %v", err)
	}
	images := imageResp.GetImages()
	if len(images) != 1 {
		t.Fatalf("GetImages() length = %d, want 1", len(images))
	}
	if got := images[0].GetKind(); got != "poster" {
		t.Fatalf("Image kind = %q, want poster", got)
	}
}

func TestMetadataServerGetImagesUsesMetadataProviderIDCache(t *testing.T) {
	dir := t.TempDir()
	movieDir := filepath.Join(dir, "movies", "Example Movie")
	media := filepath.Join(movieDir, "Example Movie.mkv")
	mustWrite(t, media, "")
	mustWrite(t, filepath.Join(movieDir, "movie.nfo"), `<movie>
  <title>Example Movie</title>
</movie>`)
	mustWrite(t, filepath.Join(movieDir, "poster.png"), "png")

	ms := &metadataServer{
		runtime: &runtimeServer{provider: provider.NewProvider()},
	}
	metadataResp, err := ms.GetMetadata(context.Background(), &pluginv1.GetMetadataRequest{
		ItemType: "movie",
		FilePath: media,
	})
	if err != nil {
		t.Fatalf("GetMetadata() error = %v", err)
	}
	item := metadataResp.GetItem()
	if item == nil {
		t.Fatal("GetMetadata().Item is nil")
	}

	imageResp, err := ms.GetImages(context.Background(), &pluginv1.GetImagesRequest{
		ProviderId:  item.GetProviderId(),
		ItemType:    "movie",
		ProviderIds: item.GetProviderIds(),
	})
	if err != nil {
		t.Fatalf("GetImages() error = %v", err)
	}
	images := imageResp.GetImages()
	if len(images) != 1 {
		t.Fatalf("GetImages() length = %d, want 1", len(images))
	}
	if got := images[0].GetKind(); got != "poster" {
		t.Fatalf("Image kind = %q, want poster", got)
	}
}

func TestMetadataServerSearchUsesRequestYearWhenFilePathNFOHasNoYear(t *testing.T) {
	dir := t.TempDir()
	movieDir := filepath.Join(dir, "movies", "Year From Folder (2024)")
	media := filepath.Join(movieDir, "Year From Folder.mkv")
	mustWrite(t, media, "")
	mustWrite(t, filepath.Join(movieDir, "movie.nfo"), `<movie>
  <title>Year From Folder</title>
</movie>`)

	providerIDs, err := stringStruct(map[string]string{"_filepath": media})
	if err != nil {
		t.Fatalf("stringStruct() error = %v", err)
	}
	ms := &metadataServer{
		runtime: &runtimeServer{provider: provider.NewProvider()},
	}
	searchResp, err := ms.Search(context.Background(), &pluginv1.SearchMetadataRequest{
		Query:       "Year From Folder",
		ItemType:    "movie",
		Year:        2024,
		ProviderIds: providerIDs,
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	results := searchResp.GetResults()
	if len(results) != 1 {
		t.Fatalf("Search() results length = %d, want 1", len(results))
	}
	if got := results[0].GetYear(); got != 2024 {
		t.Fatalf("Search result Year = %d, want 2024", got)
	}
}

func TestMetadataServerSearchSkipsUnsupportedItemType(t *testing.T) {
	t.Parallel()

	ms := &metadataServer{
		runtime: &runtimeServer{provider: provider.NewProvider()},
	}
	resp, err := ms.Search(context.Background(), &pluginv1.SearchMetadataRequest{
		Query:    "Local Movie",
		ItemType: "album",
	})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if got := len(resp.GetResults()); got != 0 {
		t.Fatalf("Search() results length = %d, want 0", got)
	}
}

func TestMetadataServerGetMetadataUsesFilePathSidecar(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	media := filepath.Join(dir, "Movie.mkv")
	mustWrite(t, media, "")
	mustWrite(t, filepath.Join(dir, "Movie.nfo"), `<movie>
  <title>Local Movie</title>
  <year>2025</year>
  <director>Local Director</director>
  <actor><name>Local Actor</name><role>Lead</role><order>1</order></actor>
</movie>`)
	mustWrite(t, filepath.Join(dir, "Movie-poster.jpg"), "jpg")

	ms := &metadataServer{
		runtime: &runtimeServer{provider: provider.NewProvider()},
	}
	resp, err := ms.GetMetadata(context.Background(), &pluginv1.GetMetadataRequest{
		ItemType: "movie",
		FilePath: media,
	})
	if err != nil {
		t.Fatalf("GetMetadata() error = %v", err)
	}
	if resp.GetItem().GetTitle() != "Local Movie" {
		t.Fatalf("Title = %q", resp.GetItem().GetTitle())
	}
	if resp.GetItem().GetYear() != 2025 {
		t.Fatalf("Year = %d", resp.GetItem().GetYear())
	}
	if resp.GetItem().GetPosterPath() == "" {
		t.Fatal("PosterPath is empty")
	}
	if got := resp.GetItem().GetProviderIds().AsMap()["local-metadata"]; got == "" {
		t.Fatalf("local-metadata provider id missing from ProviderIds: %#v", resp.GetItem().GetProviderIds().AsMap())
	}
	if got := resp.GetItem().GetProviderIds().AsMap()["local"]; got == "" {
		t.Fatalf("local provider id missing from ProviderIds: %#v", resp.GetItem().GetProviderIds().AsMap())
	}
	people := resp.GetItem().GetPeople()
	if len(people) != 2 {
		t.Fatalf("People length = %d, people = %#v", len(people), people)
	}
	byName := map[string]*pluginv1.PersonRecord{}
	for _, person := range people {
		byName[person.GetName()] = person
	}
	if got := byName["Local Actor"].GetKind(); got != "Actor" {
		t.Fatalf("Local Actor Kind = %q, want Actor", got)
	}
	if got := byName["Local Actor"].GetCharacter(); got != "Lead" {
		t.Fatalf("Local Actor Character = %q, want Lead", got)
	}
	if got := byName["Local Director"].GetKind(); got != "Director" {
		t.Fatalf("Local Director Kind = %q, want Director", got)
	}
}

func TestMetadataServerGetMetadataPreservesRequestedProviderIDForImages(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	media := filepath.Join(dir, "Movie.mkv")
	mustWrite(t, media, "")
	mustWrite(t, filepath.Join(dir, "Movie.nfo"), `<movie><title>Local Movie</title></movie>`)
	mustWrite(t, filepath.Join(dir, "poster.png"), "png")

	ms := &metadataServer{
		runtime: &runtimeServer{provider: provider.NewProvider()},
	}
	const requestedProviderID = "search-provider-id"
	metadataResp, err := ms.GetMetadata(context.Background(), &pluginv1.GetMetadataRequest{
		ProviderId: requestedProviderID,
		ItemType:   "movie",
		FilePath:   media,
	})
	if err != nil {
		t.Fatalf("GetMetadata() error = %v", err)
	}
	if got := metadataResp.GetItem().GetProviderId(); got != requestedProviderID {
		t.Fatalf("ProviderId = %q, want %q", got, requestedProviderID)
	}

	imagesResp, err := ms.GetImages(context.Background(), &pluginv1.GetImagesRequest{
		ProviderId: requestedProviderID,
		ItemType:   "movie",
	})
	if err != nil {
		t.Fatalf("GetImages() error = %v", err)
	}
	if got := len(imagesResp.GetImages()); got != 1 {
		t.Fatalf("GetImages() length = %d, want 1", got)
	}
	if got := imagesResp.GetImages()[0].GetUrl(); !strings.HasPrefix(got, "local-metadata://") {
		t.Fatalf("GetImages() URL = %q, want local-metadata:// prefix", got)
	}
}

func TestMetadataServerGetMetadataNoSidecarReturnsEmptyResponse(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	media := filepath.Join(dir, "Movie.mkv")
	mustWrite(t, media, "")

	ms := &metadataServer{
		runtime: &runtimeServer{provider: provider.NewProvider()},
	}
	resp, err := ms.GetMetadata(context.Background(), &pluginv1.GetMetadataRequest{
		ItemType: "movie",
		FilePath: media,
	})
	if err != nil {
		t.Fatalf("GetMetadata() error = %v", err)
	}
	if resp == nil {
		t.Fatal("GetMetadata() returned nil response")
	}
	if resp.GetItem() != nil {
		t.Fatalf("GetMetadata().Item = %#v, want nil", resp.GetItem())
	}
}

func mustWrite(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
