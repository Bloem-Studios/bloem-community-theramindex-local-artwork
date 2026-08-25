package provider

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/theramindex/silo-plugin-local-artwork/internal/sidecar"
)

type MetadataRequest struct {
	ContentType string
	FilePath    string
	ProviderID  string
}

type SearchRequest struct {
	ContentType string
	Query       string
	Year        int
	ProviderIDs map[string]string
}

type SearchResponse struct {
	Results []*SearchResult
}

type SearchResult struct {
	Artwork *sidecar.LookupResult
	Title   string
	Year    int
}

type ImageRequest struct {
	ContentType string
	ProviderID  string
	ProviderIDs map[string]string
}

type Provider struct {
	sidecars            *sidecar.Provider
	debug               bool
	debugLog            string
	resultsByProviderID map[string]*sidecar.LookupResult
	resultMu            sync.RWMutex
}

func NewProvider() *Provider {
	return &Provider{
		sidecars: sidecar.NewProvider(),
		debug:    debugEnabled(),
		debugLog: debugLogPath(),
	}
}

func NewProviderWithSidecars(sidecars *sidecar.Provider) *Provider {
	return &Provider{
		sidecars: sidecars,
		debug:    debugEnabled(),
		debugLog: debugLogPath(),
	}
}

func (p *Provider) GetMetadata(_ context.Context, req MetadataRequest) (*sidecar.LookupResult, error) {
	if strings.TrimSpace(req.FilePath) == "" && strings.TrimSpace(req.ProviderID) == "" {
		p.debugf("local-artwork: GetMetadata missing file_path item_type=%q", req.ContentType)
	}
	if strings.TrimSpace(req.FilePath) == "" && strings.TrimSpace(req.ProviderID) != "" {
		result, err := p.lookupCachedMetadata(req.ProviderID)
		if p.debug {
			switch {
			case err != nil:
				p.debugf("local-artwork: GetMetadata cached error item_type=%q provider_id=%q error=%v", req.ContentType, strings.TrimSpace(req.ProviderID), err)
			case result == nil:
				p.debugf("local-artwork: GetMetadata cached empty item_type=%q provider_id=%q", req.ContentType, strings.TrimSpace(req.ProviderID))
			default:
				p.debugf("local-artwork: GetMetadata cached matched item_type=%q provider_id=%q image_count=%d", req.ContentType, result.ProviderID, len(result.Images))
			}
		}
		return result, err
	}
	if p.debug {
		diag := p.sidecars.Diagnostics(req.FilePath, req.ContentType)
		p.debugf(
			"local-artwork: GetMetadata request item_type=%q file_path=%q image_count=%d",
			req.ContentType,
			diag.MediaPath,
			diag.ImageCount,
		)
	}

	result, err := p.sidecars.Lookup(req.FilePath, req.ContentType)
	if result != nil {
		if requestedProviderID := strings.TrimSpace(req.ProviderID); requestedProviderID != "" {
			aliased := *result
			aliased.ProviderID = requestedProviderID
			result = &aliased
		}
	}
	if p.debug {
		switch {
		case err != nil:
			p.debugf("local-artwork: GetMetadata error item_type=%q file_path=%q error=%v", req.ContentType, strings.TrimSpace(req.FilePath), err)
		case result == nil:
			p.debugf("local-artwork: GetMetadata empty item_type=%q file_path=%q", req.ContentType, strings.TrimSpace(req.FilePath))
		default:
			p.debugf(
				"local-artwork: GetMetadata matched item_type=%q file_path=%q provider_id=%q image_count=%d",
				req.ContentType,
				strings.TrimSpace(req.FilePath),
				result.ProviderID,
				len(result.Images),
			)
		}
	}
	p.rememberLookupResult(result)
	return result, err
}

func (p *Provider) Search(_ context.Context, req SearchRequest) (SearchResponse, error) {
	if filePath := filePathProviderID(req.ProviderIDs); filePath != "" {
		result, err := p.sidecars.Lookup(filePath, req.ContentType)
		if err != nil || result == nil {
			return SearchResponse{}, err
		}
		p.rememberLookupResult(result)
		return SearchResponse{Results: []*SearchResult{{
			Artwork: result,
			Title:   strings.TrimSpace(req.Query),
			Year:    req.Year,
		}}}, nil
	}
	return SearchResponse{}, nil
}

func (p *Provider) GetImages(_ context.Context, req ImageRequest) ([]sidecar.Image, error) {
	if filePath := filePathProviderID(req.ProviderIDs); filePath != "" {
		result, err := p.sidecars.Lookup(filePath, req.ContentType)
		if err != nil || result == nil {
			return nil, err
		}
		p.rememberLookupResult(result)
		return result.Images, nil
	}

	result, err := p.lookupCachedMetadata(providerIDFromImageRequest(req))
	if err != nil || result == nil {
		return nil, err
	}
	return result.Images, nil
}

func (p *Provider) ResolveImage(_ context.Context, path string) (string, error) {
	return p.sidecars.ResolveImage(path)
}

func (p *Provider) lookupCachedMetadata(providerID string) (*sidecar.LookupResult, error) {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return nil, nil
	}
	return p.cachedLookupResult(providerID), nil
}

func (p *Provider) cachedLookupResult(providerID string) *sidecar.LookupResult {
	p.resultMu.RLock()
	defer p.resultMu.RUnlock()
	return p.resultsByProviderID[providerID]
}

func (p *Provider) rememberLookupResult(result *sidecar.LookupResult) {
	if result == nil || strings.TrimSpace(result.ProviderID) == "" {
		return
	}
	p.resultMu.Lock()
	defer p.resultMu.Unlock()
	if p.resultsByProviderID == nil {
		p.resultsByProviderID = make(map[string]*sidecar.LookupResult)
	}
	p.resultsByProviderID[result.ProviderID] = result
}

func filePathProviderID(providerIDs map[string]string) string {
	for key, value := range providerIDs {
		normalizedKey := strings.ToLower(strings.TrimSpace(key))
		if normalizedKey != "_filepath" && normalizedKey != "file_path" && normalizedKey != "filepath" {
			continue
		}
		if filePath := strings.TrimSpace(value); filePath != "" {
			return filePath
		}
	}
	return ""
}

func providerIDFromImageRequest(req ImageRequest) string {
	if providerID := strings.TrimSpace(req.ProviderID); providerID != "" {
		return providerID
	}
	for _, key := range []string{sidecar.CapabilityID, "local"} {
		if providerID := strings.TrimSpace(req.ProviderIDs[key]); providerID != "" {
			return providerID
		}
	}
	return ""
}

func debugEnabled() bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv("SILO_LOCAL_ARTWORK_DEBUG")))
	if value == "" {
		value = strings.TrimSpace(strings.ToLower(os.Getenv("SILO_LOCAL_METADATA_DEBUG")))
	}
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

func debugLogPath() string {
	if value := strings.TrimSpace(os.Getenv("SILO_LOCAL_ARTWORK_DEBUG_LOG")); value != "" {
		return value
	}
	if value := strings.TrimSpace(os.Getenv("SILO_LOCAL_METADATA_DEBUG_LOG")); value != "" {
		return value
	}
	return "/tmp/silo-local-artwork-debug.log"
}

func (p *Provider) debugf(format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	log.Print(message)
	if !p.debug && !strings.Contains(message, "missing file_path") {
		return
	}
	if p.debugLog == "" {
		return
	}
	file, err := os.OpenFile(p.debugLog, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		log.Printf("local-artwork: open debug log %q: %v", p.debugLog, err)
		return
	}
	defer file.Close()
	_, _ = fmt.Fprintln(file, message)
}
