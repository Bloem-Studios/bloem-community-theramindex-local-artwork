package main

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"google.golang.org/protobuf/types/known/structpb"

	pluginv1 "github.com/Bloem-Studios/bloem-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
	publicmanifest "github.com/Bloem-Studios/bloem-plugin-sdk/pkg/pluginsdk/manifest"
	"github.com/Bloem-Studios/bloem-plugin-sdk/pkg/pluginsdk/runtime"
	"github.com/Bloem-Studios/bloem-community-theramindex-local-artwork/internal/sidecar"
	"github.com/Bloem-Studios/bloem-community-theramindex-local-artwork/provider"
)

// version is set at build time via -ldflags "-X main.version=...".
var version string

const localProviderIDKey = "local"

type runtimeServer struct {
	pluginv1.UnimplementedRuntimeServer

	manifest *pluginv1.PluginManifest
	provider *provider.Provider
}

type metadataServer struct {
	pluginv1.UnimplementedMetadataProviderServer
	pluginv1.UnimplementedImageResolverServer
	runtime *runtimeServer
}

//go:embed manifest.json
var manifestJSON []byte

func (s *runtimeServer) GetManifest(context.Context, *pluginv1.GetManifestRequest) (*pluginv1.GetManifestResponse, error) {
	return &pluginv1.GetManifestResponse{Manifest: s.manifest}, nil
}

func (s *runtimeServer) Configure(context.Context, *pluginv1.ConfigureRequest) (*pluginv1.ConfigureResponse, error) {
	return &pluginv1.ConfigureResponse{}, nil
}

func (s *metadataServer) Search(_ context.Context, req *pluginv1.SearchMetadataRequest) (*pluginv1.SearchMetadataResponse, error) {
	title := strings.TrimSpace(req.GetQuery())
	itemType := strings.TrimSpace(req.GetItemType())
	if !supportsSearchItemType(itemType) {
		debugf("local-artwork: Search skipped item_type=%q query=%q year=%d reason=unsupported_item_type", req.GetItemType(), req.GetQuery(), req.GetYear())
		return &pluginv1.SearchMetadataResponse{}, nil
	}
	searchResponse, err := s.runtime.provider.Search(context.Background(), provider.SearchRequest{
		ContentType: itemType,
		Query:       title,
		Year:        int(req.GetYear()),
		ProviderIDs: stringMapFromStruct(req.GetProviderIds()),
	})
	if err != nil {
		return nil, err
	}
	if len(searchResponse.Results) > 0 {
		results := make([]*pluginv1.ProviderSearchResult, 0, len(searchResponse.Results))
		for _, result := range searchResponse.Results {
			searchResult, err := providerSearchResultFromLookup(result, itemType)
			if err != nil {
				return nil, err
			}
			debugf("local-artwork: Search matched item_type=%q query=%q year=%d provider_id=%q", itemType, title, searchResult.GetYear(), searchResult.GetProviderId())
			results = append(results, searchResult)
		}
		return &pluginv1.SearchMetadataResponse{Results: results}, nil
	}
	return &pluginv1.SearchMetadataResponse{}, nil
}

func (s *metadataServer) GetMetadata(ctx context.Context, req *pluginv1.GetMetadataRequest) (*pluginv1.GetMetadataResponse, error) {
	result, err := s.runtime.provider.GetMetadata(ctx, provider.MetadataRequest{
		ContentType: req.GetItemType(),
		FilePath:    req.GetFilePath(),
		ProviderID:  req.GetProviderId(),
	})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return &pluginv1.GetMetadataResponse{}, nil
	}
	item, err := metadataItemFromResult(result, req.GetItemType())
	if err != nil {
		return nil, err
	}
	return &pluginv1.GetMetadataResponse{Item: item}, nil
}

func providerSearchResultFromLookup(result *provider.SearchResult, itemType string) (*pluginv1.ProviderSearchResult, error) {
	artwork := result.Artwork
	rawProviderIDs := map[string]string{
		localProviderIDKey:   artwork.ProviderID,
		sidecar.CapabilityID: artwork.ProviderID,
	}
	providerIDs, err := stringStruct(rawProviderIDs)
	if err != nil {
		return nil, err
	}
	return &pluginv1.ProviderSearchResult{
		ProviderId:  artwork.ProviderID,
		ItemType:    itemType,
		Title:       result.Title,
		Year:        int32(result.Year),
		ImageUrl:    searchImageURL(artwork.Images),
		ProviderIds: providerIDs,
	}, nil
}

func (s *metadataServer) GetPersonDetail(context.Context, *pluginv1.GetPersonDetailRequest) (*pluginv1.GetPersonDetailResponse, error) {
	return &pluginv1.GetPersonDetailResponse{}, nil
}

func (s *metadataServer) GetSeasons(context.Context, *pluginv1.GetSeasonsRequest) (*pluginv1.GetSeasonsResponse, error) {
	return &pluginv1.GetSeasonsResponse{}, nil
}

func (s *metadataServer) GetEpisodes(context.Context, *pluginv1.GetEpisodesRequest) (*pluginv1.GetEpisodesResponse, error) {
	return &pluginv1.GetEpisodesResponse{}, nil
}

func (s *metadataServer) GetImages(ctx context.Context, req *pluginv1.GetImagesRequest) (*pluginv1.GetImagesResponse, error) {
	images, err := s.runtime.provider.GetImages(ctx, provider.ImageRequest{
		ContentType: req.GetItemType(),
		ProviderID:  req.GetProviderId(),
		ProviderIDs: stringMapFromStruct(req.GetProviderIds()),
	})
	if err != nil {
		return nil, err
	}
	return &pluginv1.GetImagesResponse{Images: imageRecords(images)}, nil
}

func (s *metadataServer) ResolveImageURL(ctx context.Context, req *pluginv1.ResolveImageURLRequest) (*pluginv1.ResolveImageURLResponse, error) {
	url, err := s.runtime.provider.ResolveImage(ctx, req.GetPath())
	if err != nil {
		return nil, err
	}
	return &pluginv1.ResolveImageURLResponse{Url: url}, nil
}

func (s *metadataServer) ResolveImageURLs(ctx context.Context, req *pluginv1.ResolveImageURLsRequest) (*pluginv1.ResolveImageURLsResponse, error) {
	urls := make(map[string]string, len(req.GetPaths()))
	for _, path := range req.GetPaths() {
		url, err := s.runtime.provider.ResolveImage(ctx, path)
		if err != nil {
			return nil, err
		}
		urls[path] = url
	}
	return &pluginv1.ResolveImageURLsResponse{Urls: urls}, nil
}

func main() {
	manifest, err := loadManifest()
	if err != nil {
		panic(err)
	}

	rs := &runtimeServer{
		manifest: manifest,
		provider: provider.NewProvider(),
	}

	ms := &metadataServer{runtime: rs}
	runtime.Serve(runtime.ServeConfig{
		Servers: runtimeServers(rs, ms),
	})
}

func runtimeServers(rs *runtimeServer, ms *metadataServer) runtime.CapabilityServers {
	return runtime.CapabilityServers{
		Runtime:          rs,
		MetadataProvider: ms,
		ImageResolver:    ms,
	}
}

func loadManifest() (*pluginv1.PluginManifest, error) {
	manifest, err := publicmanifest.Load(manifestJSON)
	if err != nil {
		return nil, fmt.Errorf("load embedded manifest: %w", err)
	}
	if version != "" {
		manifest.Version = version
	}

	executablePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve executable path: %w", err)
	}
	binaryData, err := os.ReadFile(executablePath)
	if err != nil {
		return nil, fmt.Errorf("read executable %q: %w", executablePath, err)
	}
	checksum := sha256.Sum256(binaryData)
	manifest.Checksum = hex.EncodeToString(checksum[:])
	return manifest, nil
}

func metadataItemFromResult(result *sidecar.LookupResult, itemType string) (*pluginv1.MetadataItem, error) {
	rawProviderIDs := map[string]string{
		localProviderIDKey:   result.ProviderID,
		sidecar.CapabilityID: result.ProviderID,
	}
	providerIDs, err := stringStruct(rawProviderIDs)
	if err != nil {
		return nil, err
	}

	item := &pluginv1.MetadataItem{
		ProviderId:  result.ProviderID,
		ItemType:    itemType,
		ProviderIds: providerIDs,
	}
	for _, image := range result.Images {
		switch image.Kind {
		case "poster":
			if item.PosterPath == "" {
				item.PosterPath = sidecar.Scheme + image.Path
			}
		case "backdrop":
			if item.BackdropPath == "" {
				item.BackdropPath = sidecar.Scheme + image.Path
			}
		case "logo":
			if item.LogoPath == "" {
				item.LogoPath = sidecar.Scheme + image.Path
			}
		}
	}
	return item, nil
}

func stringStruct(value map[string]string) (*structpb.Struct, error) {
	if len(value) == 0 {
		return nil, nil
	}
	converted := make(map[string]any, len(value))
	for key, entry := range value {
		if entry != "" {
			converted[key] = entry
		}
	}
	if len(converted) == 0 {
		return nil, nil
	}
	return structpb.NewStruct(converted)
}

func stringMapFromStruct(value *structpb.Struct) map[string]string {
	if value == nil {
		return nil
	}
	out := make(map[string]string, len(value.GetFields()))
	for key, entry := range value.GetFields() {
		if stringValue := strings.TrimSpace(entry.GetStringValue()); stringValue != "" {
			out[key] = stringValue
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func searchImageURL(images []sidecar.Image) string {
	for _, image := range images {
		if image.Kind == "poster" && image.Path != "" {
			return sidecar.Scheme + image.Path
		}
	}
	for _, image := range images {
		if image.Path != "" {
			return sidecar.Scheme + image.Path
		}
	}
	return ""
}

func imageRecords(images []sidecar.Image) []*pluginv1.ImageRecord {
	if len(images) == 0 {
		return nil
	}
	records := make([]*pluginv1.ImageRecord, 0, len(images))
	for _, image := range images {
		if image.Path == "" {
			continue
		}
		records = append(records, &pluginv1.ImageRecord{
			Kind: image.Kind,
			Url:  sidecar.Scheme + image.Path,
		})
	}
	return records
}

func supportsSearchItemType(itemType string) bool {
	switch strings.ToLower(strings.TrimSpace(itemType)) {
	case "movie", "musicvideo", "music_video", "series", "show", "tvshow", "tv_show", "season", "episode":
		return true
	default:
		return false
	}
}

func debugf(format string, args ...any) {
	value := strings.TrimSpace(strings.ToLower(os.Getenv("SILO_LOCAL_ARTWORK_DEBUG")))
	if value == "" {
		value = strings.TrimSpace(strings.ToLower(os.Getenv("SILO_LOCAL_METADATA_DEBUG")))
	}
	if value != "1" && value != "true" && value != "yes" && value != "on" {
		return
	}
	path := strings.TrimSpace(os.Getenv("SILO_LOCAL_ARTWORK_DEBUG_LOG"))
	if path == "" {
		path = strings.TrimSpace(os.Getenv("SILO_LOCAL_METADATA_DEBUG_LOG"))
	}
	if path == "" {
		path = "/tmp/silo-local-artwork-debug.log"
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = fmt.Fprintf(file, format+"\n", args...)
}
