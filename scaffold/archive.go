package scaffold

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/intentd/tools/fileio"
)

// DownloadOptions configures downloading a published example pack.
type DownloadOptions struct {
	// Repository is the GitHub repository in owner/name form.
	Repository string

	// Version is the GitHub release tag.
	Version string

	// CacheDir is the directory used to store downloaded archives.
	CacheDir string

	// Refresh forces a new download, even when the archive is cached.
	Refresh bool

	// HTTPClient optionally overrides the HTTP client.
	HTTPClient *http.Client
}

// DownloadPack downloads an example pack from GitHub Releases and caches it.
// It returns the path to the cached archive.
func DownloadPack(pack string, options DownloadOptions) (string, error) {
	return downloadPackFromURL(pack, options, releaseBaseURL(options))
}

func releaseBaseURL(options DownloadOptions) string {
	return fmt.Sprintf(
		"https://github.com/%s/releases/download/%s",
		strings.Trim(options.Repository, "/"),
		url.PathEscape(options.Version),
	)
}

func downloadPackFromURL(pack string, options DownloadOptions, baseURL string) (string, error) {
	if !fs.ValidPath(pack) || pack == "." || strings.Contains(pack, "/") {
		return "", fmt.Errorf("invalid pack name %q", pack)
	}
	if options.Repository == "" {
		return "", fmt.Errorf("GitHub repository is required")
	}
	if options.Version == "" {
		return "", fmt.Errorf("release version is required")
	}
	if options.CacheDir == "" {
		return "", fmt.Errorf("cache directory is required")
	}
	if baseURL == "" {
		return "", fmt.Errorf("release base URL is required")
	}

	filename := fmt.Sprintf("examples_%s_%s.tar.gz", pack, options.Version)
	cachedPath := filepath.Join(options.CacheDir, filename)

	if err := os.MkdirAll(options.CacheDir, 0755); err != nil {
		return "", fmt.Errorf("create cache directory: %w", err)
	}

	if !options.Refresh {
		info, err := os.Stat(cachedPath)
		switch {
		case err == nil:
			if !info.Mode().IsRegular() {
				return "", fmt.Errorf("cached pack is not a regular file: %s", cachedPath)
			}
			return cachedPath, nil
		case !errors.Is(err, fs.ErrNotExist):
			return "", fmt.Errorf("inspect cached pack: %w", err)
		}
	}

	if err := downloadPack(cachedPath, filename, options, baseURL); err != nil {
		return "", err
	}

	return cachedPath, nil
}

// downloadPack writes the downloaded archive to a temporary file and atomically
// replaces the cache entry only after the download succeeds.
func downloadPack(destination, filename string, options DownloadOptions, baseURL string) error {
	releaseURL := strings.TrimRight(baseURL, "/") + "/" + url.PathEscape(filename)

	client := options.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}

	resp, err := client.Get(releaseURL)
	if err != nil {
		return fmt.Errorf("download example pack: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download example pack: server returned HTTP %d", resp.StatusCode)
	}

	tmp, err := os.CreateTemp(filepath.Dir(destination), filename+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary cache file: %w", err)
	}

	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write downloaded pack: %w", err)
	}

	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close downloaded pack: %w", err)
	}

	if err := os.Rename(tmpPath, destination); err != nil {
		return fmt.Errorf("cache downloaded pack: %w", err)
	}

	return nil
}

// ExtractPackArchive extracts a downloaded example pack into root/pack.
func ExtractPackArchive(root, pack, archivePath string) error {
	if root == "" {
		return fmt.Errorf("project root is required")
	}
	if !fs.ValidPath(pack) || pack == "." || strings.Contains(pack, "/") {
		return fmt.Errorf("invalid pack name %q", pack)
	}
	if archivePath == "" {
		return fmt.Errorf("archive path is required")
	}

	if err := fileio.CheckLocal(archivePath); err != nil {
		return fmt.Errorf("inspect archive: %w", err)
	}

	info, err := os.Stat(archivePath)
	if err != nil {
		return fmt.Errorf("inspect archive: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("archive is not a regular file: %s", archivePath)
	}

	archive, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer archive.Close()

	targetDir := filepath.Join(root, pack)
	if err := ExtractTarGz(targetDir, archive); err != nil {
		return fmt.Errorf("extract pack %q: %w", pack, err)
	}

	return nil
}

// DownloadAndExtract downloads and extracts an example pack.
// Prefer DownloadPack and ExtractPackArchive separately when the caller
// needs to report or control the two operations independently.
func DownloadAndExtract(root, pack string, options DownloadOptions) error {
	archivePath, err := DownloadPack(pack, options)
	if err != nil {
		return err
	}

	return ExtractPackArchive(root, pack, archivePath)
}

// ExtractTarGz extracts a gzip-compressed tar archive into targetDir.
// Only regular files and directories are supported.
func ExtractTarGz(targetDir string, archive io.Reader) error {
	if targetDir == "" {
		return fmt.Errorf("target directory is required")
	}
	if archive == nil {
		return fmt.Errorf("archive reader is required")
	}

	gzr, err := gzip.NewReader(archive)
	if err != nil {
		return fmt.Errorf("open gzip archive: %w", err)
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar archive: %w", err)
		}

		name, err := safeArchivePath(header.Name)
		if err != nil {
			return err
		}
		if name == "." {
			continue
		}

		dst := filepath.Join(targetDir, filepath.FromSlash(name))

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dst, 0755); err != nil {
				return fmt.Errorf("create directory %s: %w", name, err)
			}

		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
				return fmt.Errorf("create parent directory for %s: %w", name, err)
			}

			mode := fs.FileMode(0644)
			if filepath.Ext(name) == ".sh" {
				mode = 0755
			}

			file, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
			if err != nil {
				return fmt.Errorf("create file %s: %w", name, err)
			}

			_, copyErr := io.Copy(file, tr)
			closeErr := file.Close()

			if copyErr != nil {
				return fmt.Errorf("write file %s: %w", name, copyErr)
			}
			if closeErr != nil {
				return fmt.Errorf("close file %s: %w", name, closeErr)
			}

		default:
			return fmt.Errorf("unsupported archive entry %q (type %d)", header.Name, header.Typeflag)
		}
	}

	return nil
}

func safeArchivePath(name string) (string, error) {
	if name == "" || strings.Contains(name, "\\") || path.IsAbs(name) {
		return "", fmt.Errorf("archive contains an invalid path %q", name)
	}

	// Reject traversal components rather than silently normalizing them.
	for _, component := range strings.Split(name, "/") {
		if component == ".." {
			return "", fmt.Errorf("archive contains an unsafe path %q", name)
		}
	}

	clean := path.Clean(name)
	if clean == "." {
		return ".", nil
	}
	if !fs.ValidPath(clean) {
		return "", fmt.Errorf("archive contains an unsafe path %q", name)
	}

	return clean, nil
}

// ClearCache removes all entries from cacheDir, retaining the directory.
func ClearCache(cacheDir string) error {
	if cacheDir == "" {
		return fmt.Errorf("cache directory is required")
	}

	info, err := os.Stat(cacheDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect cache directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("cache path is not a directory: %s", cacheDir)
	}

	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return fmt.Errorf("read cache directory: %w", err)
	}

	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(cacheDir, entry.Name())); err != nil {
			return fmt.Errorf("remove cache entry %q: %w", entry.Name(), err)
		}
	}

	return nil
}
