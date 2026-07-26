package provider

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/theramindex/silo-plugin-local-metadata/internal/sidecar"
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
	Results         []*sidecar.LookupResult
	Authoritative   bool
	IndexConfigured bool
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
	indexOnce           sync.Once
	index               *localIndex
	indexErr            error
	resultsByProviderID map[string]*sidecar.LookupResult
	resultMu            sync.RWMutex
}

type localIndex struct {
	entries      []localIndexEntry
	byProviderID map[string]*sidecar.LookupResult
}

type localIndexEntry struct {
	result *sidecar.LookupResult
	keys   []string
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
		p.debugf("local-metadata: GetMetadata missing file_path item_type=%q", req.ContentType)
	}
	if strings.TrimSpace(req.FilePath) == "" && strings.TrimSpace(req.ProviderID) != "" {
		result, err := p.lookupCachedMetadata(req.ProviderID)
		if p.debug {
			switch {
			case err != nil:
				p.debugf("local-metadata: GetMetadata indexed error item_type=%q provider_id=%q error=%v", req.ContentType, strings.TrimSpace(req.ProviderID), err)
			case result == nil:
				p.debugf("local-metadata: GetMetadata indexed empty item_type=%q provider_id=%q", req.ContentType, strings.TrimSpace(req.ProviderID))
			default:
				p.debugf("local-metadata: GetMetadata indexed matched item_type=%q provider_id=%q title=%q year=%d image_count=%d", req.ContentType, result.ProviderID, result.Item.Title, result.Item.Year, len(result.Images))
			}
		}
		return result, err
	}
	if p.debug {
		diag := p.sidecars.Diagnostics(req.FilePath, req.ContentType)
		p.debugf(
			"local-metadata: GetMetadata request item_type=%q file_path=%q nfo_found=%q nfo_candidates=%q image_count=%d",
			req.ContentType,
			diag.MediaPath,
			diag.NFOPath,
			strings.Join(diag.NFOCandidates, "|"),
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
			p.debugf("local-metadata: GetMetadata error item_type=%q file_path=%q error=%v", req.ContentType, strings.TrimSpace(req.FilePath), err)
		case result == nil:
			p.debugf("local-metadata: GetMetadata empty item_type=%q file_path=%q", req.ContentType, strings.TrimSpace(req.FilePath))
		default:
			p.debugf(
				"local-metadata: GetMetadata matched item_type=%q file_path=%q provider_id=%q title=%q year=%d image_count=%d",
				req.ContentType,
				strings.TrimSpace(req.FilePath),
				result.ProviderID,
				result.Item.Title,
				result.Item.Year,
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
			return SearchResponse{Authoritative: true}, err
		}
		result = searchResultWithFallbacks(result, req.Query, req.Year)
		p.rememberLookupResult(result)
		return SearchResponse{Results: []*sidecar.LookupResult{result}, Authoritative: true}, nil
	}
	if !supportsIndexedItemType(req.ContentType) {
		return SearchResponse{}, nil
	}
	index, configured, err := p.localIndex()
	if err != nil || !configured {
		return SearchResponse{IndexConfigured: configured}, err
	}
	queryKey := normalizeSearchText(req.Query)
	if queryKey == "" {
		return SearchResponse{Authoritative: true, IndexConfigured: true}, nil
	}
	matches := make([]localIndexEntry, 0, 5)
	for _, entry := range index.entries {
		if entryMatchesQuery(entry, queryKey, req.Year) {
			matches = append(matches, entry)
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		return searchRank(matches[i], queryKey) > searchRank(matches[j], queryKey)
	})
	results := make([]*sidecar.LookupResult, 0, len(matches))
	for _, match := range matches {
		result := searchResultWithFallbacks(match.result, req.Query, req.Year)
		p.rememberLookupResult(result)
		results = append(results, result)
		if len(results) >= 5 {
			break
		}
	}
	return SearchResponse{Results: results, Authoritative: true, IndexConfigured: true}, nil
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
	if result := p.cachedLookupResult(providerID); result != nil {
		return result, nil
	}
	index, configured, err := p.localIndex()
	if err != nil || !configured {
		return nil, err
	}
	return index.byProviderID[providerID], nil
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

func (p *Provider) localIndex() (*localIndex, bool, error) {
	roots := metadataRoots()
	if len(roots) == 0 {
		return nil, false, nil
	}
	p.indexOnce.Do(func() {
		p.index, p.indexErr = p.buildLocalIndex(roots)
	})
	return p.index, true, p.indexErr
}

func (p *Provider) buildLocalIndex(roots []string) (*localIndex, error) {
	index := &localIndex{
		byProviderID: make(map[string]*sidecar.LookupResult),
	}
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d == nil || d.IsDir() || !strings.EqualFold(d.Name(), "movie.nfo") {
				return nil
			}
			mediaPath := firstMediaFile(filepath.Dir(path))
			if mediaPath == "" {
				return nil
			}
			result, err := p.sidecars.Lookup(mediaPath, "movie")
			if err != nil || result == nil {
				return err
			}
			keys := indexKeys(result, mediaPath)
			if len(keys) == 0 {
				return nil
			}
			index.entries = append(index.entries, localIndexEntry{result: result, keys: keys})
			index.byProviderID[result.ProviderID] = result
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return index, nil
}

func metadataRoots() []string {
	value := strings.TrimSpace(os.Getenv("SILO_LOCAL_METADATA_ROOTS"))
	if value == "" {
		return nil
	}
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == '\n'
	})
	roots := make([]string, 0, len(parts))
	for _, part := range parts {
		if root := strings.TrimSpace(part); root != "" {
			roots = append(roots, root)
		}
	}
	return roots
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

func searchResultWithFallbacks(result *sidecar.LookupResult, query string, year int) *sidecar.LookupResult {
	if result == nil {
		return nil
	}
	adjusted := *result
	adjusted.Item = result.Item
	if adjusted.Item.Title == "" {
		adjusted.Item.Title = strings.TrimSpace(query)
	}
	if adjusted.Item.Year == 0 && year > 0 {
		adjusted.Item.Year = year
	}
	return &adjusted
}

func firstMediaFile(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if isMediaFile(path) {
			return path
		}
	}
	return ""
}

func isMediaFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mkv", ".mp4", ".avi", ".mov", ".m4v":
		return true
	default:
		return false
	}
}

func supportsIndexedItemType(itemType string) bool {
	switch strings.ToLower(strings.TrimSpace(itemType)) {
	case "movie", "musicvideo", "music_video":
		return true
	default:
		return false
	}
}

func indexKeys(result *sidecar.LookupResult, mediaPath string) []string {
	values := []string{
		result.Item.Title,
		result.Item.OriginalTitle,
		result.Item.SortTitle,
		strings.TrimSuffix(filepath.Base(mediaPath), filepath.Ext(mediaPath)),
		filepath.Base(filepath.Dir(mediaPath)),
	}
	seen := map[string]bool{}
	keys := make([]string, 0, len(values))
	for _, value := range values {
		key := normalizeSearchText(value)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		keys = append(keys, key)
	}
	return keys
}

func entryMatchesQuery(entry localIndexEntry, queryKey string, year int) bool {
	if year > 0 && entry.result.Item.Year > 0 && entry.result.Item.Year != year {
		return false
	}
	for _, key := range entry.keys {
		if key == queryKey || strings.Contains(queryKey, key) || strings.Contains(key, queryKey) {
			return true
		}
	}
	return false
}

func searchRank(entry localIndexEntry, queryKey string) int {
	best := 0
	for _, key := range entry.keys {
		switch {
		case key == queryKey:
			if best < 3 {
				best = 3
			}
		case strings.Contains(queryKey, key):
			if best < 2 {
				best = 2
			}
		case strings.Contains(key, queryKey):
			if best < 1 {
				best = 1
			}
		}
	}
	return best
}

func normalizeSearchText(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	var b strings.Builder
	depthSquare := 0
	depthCurly := 0
	for _, r := range value {
		switch r {
		case '[':
			depthSquare++
			continue
		case ']':
			if depthSquare > 0 {
				depthSquare--
			}
			continue
		case '{':
			depthCurly++
			continue
		case '}':
			if depthCurly > 0 {
				depthCurly--
			}
			continue
		}
		if depthSquare > 0 || depthCurly > 0 {
			continue
		}
		if unicode.IsSpace(r) || r == '_' || r == '.' {
			b.WriteByte(' ')
			continue
		}
		b.WriteRune(r)
	}
	fields := strings.Fields(b.String())
	if len(fields) == 0 {
		return ""
	}
	return strings.Join(fields, " ")
}

func debugEnabled() bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv("SILO_LOCAL_METADATA_DEBUG")))
	return value == "1" || value == "true" || value == "yes" || value == "on"
}

func debugLogPath() string {
	if value := strings.TrimSpace(os.Getenv("SILO_LOCAL_METADATA_DEBUG_LOG")); value != "" {
		return value
	}
	return "/tmp/silo-local-metadata-debug.log"
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
		log.Printf("local-metadata: open debug log %q: %v", p.debugLog, err)
		return
	}
	defer file.Close()
	_, _ = fmt.Fprintln(file, message)
}
