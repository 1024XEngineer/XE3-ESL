package meme

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	stdimage "image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "golang.org/x/image/webp"
)

const maxManifestBytes = 2 * 1024 * 1024

type manifest struct {
	PackID      string             `json:"pack_id"`
	Version     string             `json:"version"`
	License     string             `json:"license"`
	Attribution string             `json:"attribution"`
	Categories  []manifestCategory `json:"categories"`
	Assets      []manifestAsset    `json:"assets"`
}

type manifestCategory struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}

type manifestAsset struct {
	ID          string `json:"id"`
	Category    string `json:"category"`
	Path        string `json:"path"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	SHA256      string `json:"sha256"`
	Weight      int    `json:"weight"`
}

// FileCatalog is an immutable, startup-validated local meme pack. Runtime
// selection never scans directories and clients never receive absolute paths.
type FileCatalog struct {
	root       string
	packID     string
	version    string
	categories []CategoryDefinition
	byCategory map[Category][]Asset
	byKey      map[string]Asset
}

func NewFileCatalog(root, packID, version string) (*FileCatalog, error) {
	if strings.TrimSpace(root) == "" || packID == "" || version == "" {
		return nil, ErrInvalidRequest
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, ErrInvalidRequest
	}
	packRoot := filepath.Join(absoluteRoot, packID, version)
	manifestPath := filepath.Join(packRoot, "manifest.json")
	file, err := os.Open(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("%w: manifest", ErrNotFound)
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, maxManifestBytes+1))
	decoder.DisallowUnknownFields()
	var document manifest
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("%w: manifest", ErrInvalidRequest)
	}
	if document.PackID != packID || document.Version != version ||
		strings.TrimSpace(document.License) == "" ||
		strings.TrimSpace(document.Attribution) == "" ||
		len(document.Categories) == 0 || len(document.Assets) == 0 {
		return nil, fmt.Errorf("%w: manifest metadata", ErrInvalidRequest)
	}

	catalog := &FileCatalog{
		root:       absoluteRoot,
		packID:     packID,
		version:    version,
		byCategory: make(map[Category][]Asset),
		byKey:      make(map[string]Asset),
	}
	knownCategories := make(map[Category]struct{}, len(document.Categories))
	for _, item := range document.Categories {
		category := Category(item.ID)
		if !validStableID(item.ID) || strings.TrimSpace(item.Description) == "" {
			return nil, fmt.Errorf("%w: category", ErrInvalidRequest)
		}
		if _, duplicate := knownCategories[category]; duplicate {
			return nil, fmt.Errorf("%w: duplicate category", ErrInvalidRequest)
		}
		knownCategories[category] = struct{}{}
		catalog.categories = append(catalog.categories, CategoryDefinition{
			Category: category, Description: strings.TrimSpace(item.Description),
		})
	}
	sort.Slice(catalog.categories, func(i, j int) bool {
		return catalog.categories[i].Category < catalog.categories[j].Category
	})
	seenIDs := make(map[string]struct{}, len(document.Assets))
	for _, item := range document.Assets {
		category := Category(item.Category)
		if _, known := knownCategories[category]; !known ||
			!validStableID(item.ID) || !validRelativePath(item.Path) ||
			!validContentType(item.ContentType) || item.Weight <= 0 {
			return nil, fmt.Errorf("%w: asset metadata", ErrInvalidRequest)
		}
		if _, duplicate := seenIDs[item.ID]; duplicate {
			return nil, fmt.Errorf("%w: duplicate asset", ErrInvalidRequest)
		}
		seenIDs[item.ID] = struct{}{}
		assetKey := filepath.ToSlash(filepath.Join(packID, version, item.Path))
		absolutePath, err := catalog.resolve(assetKey)
		if err != nil {
			return nil, err
		}
		verified, err := verifyAsset(absolutePath, item)
		if err != nil {
			return nil, fmt.Errorf("%w: asset %s", err, item.ID)
		}
		asset := Asset{
			MemeID: item.ID, PackID: packID, PackVersion: version,
			Category: category, AssetKey: assetKey,
			ContentType: item.ContentType, SizeBytes: verified.size,
			Width: verified.width, Height: verified.height,
			ChecksumSHA256: item.SHA256, Weight: item.Weight,
		}
		catalog.byCategory[category] = append(catalog.byCategory[category], asset)
		catalog.byKey[assetKey] = asset
	}
	return catalog, nil
}

func (catalog *FileCatalog) Categories(
	_ context.Context,
	packID string,
	version string,
) ([]CategoryDefinition, error) {
	if catalog == nil || packID != catalog.packID || version != catalog.version {
		return nil, ErrNotFound
	}
	return append([]CategoryDefinition(nil), catalog.categories...), nil
}

func (catalog *FileCatalog) Candidates(
	_ context.Context,
	packID string,
	version string,
	category Category,
) ([]Asset, error) {
	if catalog == nil || packID != catalog.packID || version != catalog.version {
		return nil, ErrNotFound
	}
	assets := catalog.byCategory[category]
	if len(assets) == 0 {
		return nil, ErrNotFound
	}
	return append([]Asset(nil), assets...), nil
}

// Open returns only an asset already admitted by the startup manifest.
func (catalog *FileCatalog) Open(assetKey string) (*os.File, Asset, error) {
	if catalog == nil {
		return nil, Asset{}, ErrNotFound
	}
	asset, ok := catalog.byKey[assetKey]
	if !ok {
		return nil, Asset{}, ErrNotFound
	}
	absolutePath, err := catalog.resolve(assetKey)
	if err != nil {
		return nil, Asset{}, err
	}
	file, err := os.Open(absolutePath)
	if err != nil {
		return nil, Asset{}, ErrNotFound
	}
	return file, asset, nil
}

func (catalog *FileCatalog) resolve(assetKey string) (string, error) {
	if !validRelativePath(assetKey) {
		return "", ErrInvalidRequest
	}
	absolutePath := filepath.Join(catalog.root, filepath.FromSlash(assetKey))
	relative, err := filepath.Rel(catalog.root, absolutePath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", ErrInvalidRequest
	}
	return absolutePath, nil
}

type verifiedAsset struct {
	size   int64
	width  int
	height int
}

func verifyAsset(path string, expected manifestAsset) (verifiedAsset, error) {
	file, err := os.Open(path)
	if err != nil {
		return verifiedAsset{}, ErrNotFound
	}
	defer file.Close()
	hash := sha256.New()
	count, err := io.Copy(hash, file)
	if err != nil {
		return verifiedAsset{}, ErrRepository
	}
	if count != expected.SizeBytes ||
		hex.EncodeToString(hash.Sum(nil)) != expected.SHA256 {
		return verifiedAsset{}, ErrInvalidRequest
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return verifiedAsset{}, ErrRepository
	}
	configuration, format, err := stdimage.DecodeConfig(file)
	if err != nil || configuration.Width != expected.Width ||
		configuration.Height != expected.Height ||
		contentTypeForFormat(format) != expected.ContentType {
		return verifiedAsset{}, ErrInvalidRequest
	}
	return verifiedAsset{size: count, width: configuration.Width, height: configuration.Height}, nil
}

func contentTypeForFormat(format string) string {
	switch format {
	case "gif":
		return "image/gif"
	case "jpeg":
		return "image/jpeg"
	case "png":
		return "image/png"
	case "webp":
		return "image/webp"
	default:
		return ""
	}
}

func validStableID(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for index, r := range value {
		if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' ||
			r >= '0' && r <= '9' || index > 0 && strings.ContainsRune("._:-", r) {
			continue
		}
		return false
	}
	return true
}

func validRelativePath(value string) bool {
	return value != "" && len(value) <= 1024 && value[0] != '/' &&
		!strings.Contains(value, `\`) && filepath.ToSlash(filepath.Clean(value)) == value &&
		value != "." && value != ".." && !strings.HasPrefix(value, "../")
}

func validContentType(value string) bool {
	return contentTypeForFormat(strings.TrimPrefix(value, "image/")) != "" ||
		value == "image/jpeg"
}

var _ Catalog = (*FileCatalog)(nil)
