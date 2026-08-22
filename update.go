package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// GitHubRelease describes a GitHub release asset for version checking.
type GitHubRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
		Size               int64  `json:"size"`
	} `json:"assets"`
}

// checkForUpdate queries GitHub Releases for the latest version.
// Returns (release, downloadURL, error). downloadURL is empty if no update.
func checkForUpdate(client *http.Client) (*GitHubRelease, string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", GitHubOwner, GitHubRepo)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("не удалось проверить обновления: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, "", nil // no releases yet
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, "", fmt.Errorf("GitHub %d: %s", resp.StatusCode, string(body))
	}

	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, "", err
	}

	remoteVer := normalizeVersion(release.TagName)
	localVer := normalizeVersion(AppVersion)
	if remoteVer == "" || localVer == "" {
		return &release, "", nil
	}
	if remoteVer <= localVer {
		return &release, "", nil // no update
	}

	// Find matching exe asset for this platform.
	for _, a := range release.Assets {
		lower := strings.ToLower(a.Name)
		if strings.Contains(lower, "amd64") || strings.Contains(lower, "x64") {
			if strings.HasSuffix(lower, ".exe") || strings.HasSuffix(lower, ".zip") {
				return &release, a.BrowserDownloadURL, nil
			}
		}
	}
	// fallback: any .exe or .zip
	for _, a := range release.Assets {
		lower := strings.ToLower(a.Name)
		if strings.HasSuffix(lower, ".exe") {
			return &release, a.BrowserDownloadURL, nil
		}
	}

	return &release, "", nil
}

// downloadUpdate downloads the file from URL and saves it to destPath.
// onProgress is called with bytes downloaded / total bytes (0,0 if total unknown).
func downloadUpdate(client *http.Client, url string, destPath string, onProgress func(downloaded, total int64)) error {
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("ошибка загрузки: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("загрузка вернула %d", resp.StatusCode)
	}

	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}

	total := resp.ContentLength
	tmpPath := destPath + ".tmp"
	out, err := os.Create(tmpPath)
	if err != nil {
		return err
	}

	buf := make([]byte, 32*1024)
	var downloaded int64
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, wErr := out.Write(buf[:n]); wErr != nil {
				out.Close()
				os.Remove(tmpPath)
				return fmt.Errorf("ошибка записи: %w", wErr)
			}
			downloaded += int64(n)
			if onProgress != nil {
				onProgress(downloaded, total)
			}
		}
		if readErr != nil {
			break
		}
	}
	out.Close()
	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("ошибка чтения: %w", err)
	}

	if err := os.Rename(tmpPath, destPath); err != nil {
		return err
	}
	return nil
}

// versionFilePath returns the path to the downloaded update exe next to the current exe.
func versionFilePath(version string) string {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Sprintf("MOEX_Volumes_%s.exe", version)
	}
	dir := filepath.Dir(exe)
	ext := filepath.Ext(exe)
	base := strings.TrimSuffix(filepath.Base(exe), ext)
	return filepath.Join(dir, fmt.Sprintf("%s_%s%s", base, version, ext))
}

// normalizeVersion strips leading "v" and trims whitespace for comparison.
func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	v = strings.TrimPrefix(v, "V")
	return v
}

func newHTTPClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second}
}
