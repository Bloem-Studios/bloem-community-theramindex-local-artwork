package sidecar

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	// CapabilityID remains stable so existing Silo provider assignments upgrade
	// in place even though the product is now named Local Artwork.
	CapabilityID = "local-metadata"
	Scheme       = "local-artwork://"
	LegacyScheme = "local-metadata://"

	maxDataURLImageBytes = 8 * 1024 * 1024
)

var supportedImageExtensions = [...]string{".png", ".jpg", ".jpeg", ".webp"}

type LookupResult struct {
	ProviderID string
	Images     []Image
}

type Image struct {
	Kind string
	Path string
}

type Provider struct{}

type Diagnostics struct {
	MediaPath  string
	ItemType   string
	ImageCount int
}

func NewProvider() *Provider {
	return &Provider{}
}

func (p *Provider) Lookup(mediaPath string, _ ...string) (*LookupResult, error) {
	mediaPath, ok := safeLookupPath(mediaPath)
	if !ok {
		return nil, nil
	}
	images := p.findImages(mediaPath)
	if len(images) == 0 {
		return nil, nil
	}
	return &LookupResult{
		ProviderID: providerID(mediaPath),
		Images:     images,
	}, nil
}

func (p *Provider) Diagnostics(mediaPath, itemType string) Diagnostics {
	mediaPath = strings.TrimSpace(mediaPath)
	return Diagnostics{
		MediaPath:  mediaPath,
		ItemType:   strings.ToLower(strings.TrimSpace(itemType)),
		ImageCount: len(p.findImages(mediaPath)),
	}
}

func (p *Provider) ResolveImage(path string) (string, error) {
	localPath := strings.TrimSpace(path)
	if localPath == "" {
		return "", nil
	}
	localPath = strings.TrimPrefix(localPath, Scheme)
	localPath = strings.TrimPrefix(localPath, LegacyScheme)
	localPath, ok := p.safeImagePath(localPath)
	if !ok {
		return "", nil
	}

	rc, err := os.Open(localPath)
	if err != nil {
		return "", err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, maxDataURLImageBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) == 0 || len(data) > maxDataURLImageBytes {
		return "", nil
	}
	mimeType := http.DetectContentType(data)
	if !strings.HasPrefix(mimeType, "image/") {
		return "", nil
	}
	return "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

func (p *Provider) findImages(mediaPath string) []Image {
	candidates := p.imageCandidates(mediaPath)
	images := make([]Image, 0, len(candidates))
	variantSelected := false
	for _, candidate := range candidates {
		if candidate.posterVariant && variantSelected {
			continue
		}
		candidate.path = actualCasePath(candidate.path)
		if resolved, ok := p.safeImagePath(candidate.path); ok && hasImageContent(resolved) {
			images = append(images, Image{Kind: candidate.kind, Path: candidate.path})
			if candidate.posterVariant {
				variantSelected = true
			}
		}
	}
	return images
}

type imageCandidate struct {
	kind          string
	path          string
	posterVariant bool
}

func (p *Provider) imageCandidates(mediaPath string) []imageCandidate {
	base := trimExt(mediaPath)
	var out []imageCandidate
	for _, spec := range []struct {
		kind   string
		suffix []string
	}{
		{"poster", []string{"-poster", ".poster"}},
		{"backdrop", []string{"-backdrop", ".backdrop", "-fanart", ".fanart"}},
		{"logo", []string{"-logo", ".logo"}},
		{"still", []string{"-thumb", ".thumb", "-still", ".still"}},
	} {
		for _, suffix := range spec.suffix {
			for _, ext := range supportedImageExtensions {
				out = append(out, imageCandidate{kind: spec.kind, path: base + suffix + ext})
			}
		}
	}

	dir := sidecarDir(mediaPath)
	for _, spec := range []struct {
		kind  string
		names []string
	}{
		{"poster", []string{"poster", "folder"}},
		{"backdrop", []string{"backdrop", "fanart"}},
		{"logo", []string{"logo", "clearlogo"}},
		{"still", []string{"thumb", "still"}},
	} {
		for _, name := range spec.names {
			for _, ext := range supportedImageExtensions {
				out = append(out, imageCandidate{kind: spec.kind, path: filepath.Join(dir, name+ext)})
			}
		}
	}
	out = append(out, folderPosterVariantCandidates(dir)...)
	return dedupeImageCandidates(out)
}

func folderPosterVariantCandidates(dir string) []imageCandidate {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	sort.Slice(entries, func(i, j int) bool {
		left := strings.ToLower(entries[i].Name())
		right := strings.ToLower(entries[j].Name())
		if left == right {
			return entries[i].Name() < entries[j].Name()
		}
		return left < right
	})
	var candidates []imageCandidate
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if !isSupportedImageExtension(ext) {
			continue
		}
		stem := strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name)))
		if strings.HasPrefix(stem, "poster-") && len(stem) > len("poster-") {
			candidates = append(candidates, imageCandidate{
				kind:          "poster",
				path:          filepath.Join(dir, name),
				posterVariant: true,
			})
		}
	}
	return candidates
}

func (p *Provider) safeImagePath(path string) (string, bool) {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "." || !filepath.IsAbs(path) || !isSupportedImageExtension(strings.ToLower(filepath.Ext(path))) {
		return "", false
	}
	leaf, err := os.Lstat(path)
	if err != nil || leaf.Mode()&os.ModeSymlink != 0 || !leaf.Mode().IsRegular() || leaf.Size() <= 0 || leaf.Size() > maxDataURLImageBytes {
		return "", false
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || !p.belongsToSiblingMedia(resolved) {
		return "", false
	}
	return resolved, true
}

func (p *Provider) belongsToSiblingMedia(imagePath string) bool {
	dir := filepath.Dir(imagePath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.IsDir() || !isMediaFile(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		mediaPath := filepath.Join(dir, entry.Name())
		if candidateMatchesImage(p.imageCandidates(mediaPath), imagePath) {
			return true
		}
	}
	return hasMediaWithin(dir, 2) && candidateMatchesImage(p.imageCandidates(dir), imagePath)
}

func candidateMatchesImage(candidates []imageCandidate, imagePath string) bool {
	for _, candidate := range candidates {
		candidatePath, err := filepath.EvalSymlinks(actualCasePath(candidate.path))
		if err == nil && candidatePath == imagePath {
			return true
		}
	}
	return false
}

func hasMediaWithin(dir string, remainingDepth int) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if entry.IsDir() {
			if remainingDepth > 0 && hasMediaWithin(path, remainingDepth-1) {
				return true
			}
			continue
		}
		if !isMediaFile(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err == nil && info.Mode().IsRegular() {
			return true
		}
	}
	return false
}

func hasImageContent(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return false
	}
	return strings.HasPrefix(http.DetectContentType(buf[:n]), "image/")
}

func actualCasePath(path string) string {
	dir := filepath.Dir(path)
	want := filepath.Base(path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return path
	}
	for _, entry := range entries {
		if entry.Name() == want {
			return filepath.Join(dir, entry.Name())
		}
	}
	for _, entry := range entries {
		if strings.EqualFold(entry.Name(), want) {
			return filepath.Join(dir, entry.Name())
		}
	}
	return path
}

func sidecarDir(mediaPath string) string {
	if info, err := os.Stat(mediaPath); err == nil && info.IsDir() {
		return mediaPath
	}
	return filepath.Dir(mediaPath)
}

func trimExt(path string) string {
	return strings.TrimSuffix(path, filepath.Ext(path))
}

func isSupportedImageExtension(ext string) bool {
	for _, supported := range supportedImageExtensions {
		if ext == supported {
			return true
		}
	}
	return false
}

func isMediaFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mkv", ".mp4", ".avi", ".mov", ".m4v", ".webm",
		".mpg", ".mpeg", ".wmv", ".ts", ".m2ts", ".mts", ".iso",
		".vob", ".ogm", ".ogv", ".flv", ".f4v", ".3gp", ".3g2",
		".asf", ".divx", ".rm", ".rmvb":
		return true
	default:
		return false
	}
}

func safeLookupPath(path string) (string, bool) {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "." || !filepath.IsAbs(path) {
		return "", false
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		return "", false
	}
	if !info.IsDir() && (!info.Mode().IsRegular() || !isMediaFile(path)) {
		return "", false
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", false
	}
	return resolved, true
}

func dedupeImageCandidates(values []imageCandidate) []imageCandidate {
	seen := make(map[string]bool, len(values))
	out := make([]imageCandidate, 0, len(values))
	for _, value := range values {
		key := value.kind + "\x00" + value.path
		if value.path == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, value)
	}
	return out
}

func providerID(mediaPath string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(mediaPath)))
	return hex.EncodeToString(sum[:])[:24]
}
