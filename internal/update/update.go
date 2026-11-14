package update

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bestruirui/octopus/internal/conf"
	"github.com/bestruirui/octopus/internal/rhttp"
	"github.com/charmbracelet/log"
	"golang.org/x/mod/semver"
)

type LatestInfo struct {
	TagName         string `json:"tag_name"`
	PublishedAt     string `json:"published_at"`
	Body            string `json:"body"`
	HTMLURL         string `json:"html_url"`
	Status          string `json:"status"`
	UpdateAvailable bool   `json:"update_available"`
}

type githubRelease struct {
	TagName     string `json:"tag_name"`
	PublishedAt string `json:"published_at"`
	Body        string `json:"body"`
	Draft       bool   `json:"draft"`
	Prerelease  bool   `json:"prerelease"`
	Assets      []struct {
		Name string `json:"name"`
	} `json:"assets"`
}

type httpStatusError struct {
	StatusCode int
}

func (err *httpStatusError) Error() string {
	return fmt.Sprintf("GitHub request failed: HTTP %d (%s)", err.StatusCode, http.StatusText(err.StatusCode))
}

var ErrUpdateUnavailable = errors.New("update unavailable")

type releaseRequest func(string) ([]byte, error)

// doRequestWithFallback performs an HTTP GET request, first without proxy, then with proxy if failed.
func doRequestWithFallback(url string) ([]byte, error) {
	data, err := doRequest(url, false)
	if err == nil {
		return data, nil
	}
	var statusError *httpStatusError
	if errors.As(err, &statusError) && statusError.StatusCode < 500 {
		return nil, err
	}
	log.Warnf("direct request failed, trying with proxy: %v", err)
	data, proxyErr := doRequest(url, true)
	if proxyErr != nil {
		return nil, errors.Join(err, proxyErr)
	}
	return data, nil
}

func doRequest(url string, useProxy bool) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var hc *http.Client
	var err error
	if useProxy {
		hc, err = rhttp.Proxy()
	} else {
		hc, err = rhttp.Direct()
	}
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		log.Debugf("new request failed: %v", err)
		return nil, err
	}

	if token := os.Getenv(strings.ToUpper(conf.APP_NAME) + "_GITHUB_PAT"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("User-Agent", conf.APP_NAME+"/"+conf.Version)

	resp, err := hc.Do(req)
	if err != nil {
		log.Debugf("request failed: %v", err)
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &httpStatusError{StatusCode: resp.StatusCode}
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Debugf("read body failed: %v", err)
		return nil, err
	}
	return data, nil
}

func GetLatestInfo() (*LatestInfo, error) {
	return getLatestInfo(conf.Version, doRequestWithFallback)
}

func getLatestInfo(currentVersion string, request releaseRequest) (*LatestInfo, error) {
	release, err := fetchRelease(conf.ReleasesAPIURL()+"/latest", request)
	var statusError *httpStatusError
	if errors.As(err, &statusError) && statusError.StatusCode == http.StatusNotFound {
		return &LatestInfo{Status: "no_release"}, nil
	}
	if err != nil {
		return nil, err
	}
	if release.Draft || release.Prerelease {
		return &LatestInfo{Status: "no_release"}, nil
	}
	if !isStableVersion(release.TagName) {
		return nil, fmt.Errorf("invalid stable release version: %q", release.TagName)
	}
	info := &LatestInfo{
		TagName:     release.TagName,
		PublishedAt: release.PublishedAt,
		Body:        release.Body,
		HTMLURL:     conf.RepositoryURL() + "/releases/tag/" + url.PathEscape(release.TagName),
		Status:      "up_to_date",
	}
	if !isStableVersion(currentVersion) {
		info.Status = "development"
		return info, nil
	}
	if semver.Compare(release.TagName, currentVersion) <= 0 {
		return info, nil
	}
	filename, err := getDownloadFilename()
	if err != nil {
		info.Status = "unsupported_platform"
	} else if !release.hasAsset(filename) {
		info.Status = "asset_missing"
	} else {
		info.Status = "update_available"
		info.UpdateAvailable = true
	}
	return info, nil
}

func fetchRelease(endpoint string, request releaseRequest) (*githubRelease, error) {
	body, err := request(endpoint)
	if err != nil {
		return nil, err
	}
	var release githubRelease
	if err := json.Unmarshal(body, &release); err != nil {
		return nil, fmt.Errorf("decode GitHub release: %w", err)
	}
	return &release, nil
}

func isStableVersion(version string) bool {
	return semver.IsValid(version) && semver.Prerelease(version) == ""
}

func (release *githubRelease) hasAsset(filename string) bool {
	for _, asset := range release.Assets {
		if asset.Name == filename {
			return true
		}
	}
	return false
}

// resolveDownloadURL rechecks the selected release, so a later publication cannot
// change the version installed by an already-open settings page.
func resolveDownloadURL(tag, currentVersion string, request releaseRequest) (string, error) {
	if !isStableVersion(currentVersion) || !isStableVersion(tag) || semver.Compare(tag, currentVersion) <= 0 {
		return "", fmt.Errorf("%w: select a newer stable release from a stable installation", ErrUpdateUnavailable)
	}
	release, err := fetchRelease(conf.ReleasesAPIURL()+"/tags/"+url.PathEscape(tag), request)
	if err != nil {
		return "", err
	}
	if release.TagName != tag || release.Draft || release.Prerelease {
		return "", fmt.Errorf("%w: the selected stable release is no longer available", ErrUpdateUnavailable)
	}
	filename, err := getDownloadFilename()
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUpdateUnavailable, err)
	}
	if !release.hasAsset(filename) {
		return "", fmt.Errorf("%w: the selected release has no archive for this platform", ErrUpdateUnavailable)
	}
	return conf.RepositoryURL() + "/releases/download/" + url.PathEscape(tag) + "/" + filename, nil
}

func unzip(data []byte, dest string) error {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		log.Debugf("new zip reader failed: %v", err)
		return err
	}

	for _, f := range r.File {
		fpath := filepath.Join(dest, f.Name)

		if !isPathInDest(fpath, dest) {
			log.Debugf("invalid file path: %s", fpath)
			return fmt.Errorf("invalid file path: %s", fpath)
		}

		info := f.FileInfo()
		if info.IsDir() {
			os.MkdirAll(fpath, os.ModePerm)
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			continue
		}

		if err := extractFile(f, fpath); err != nil {
			return err
		}
	}
	return nil
}

func extractFile(f *zip.File, fpath string) error {
	if err := os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
		log.Debugf("mkdir all failed: %v", err)
		return err
	}

	outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode().Perm())
	if err != nil {
		if err = os.Remove(fpath); err != nil {
			log.Debugf("remove file failed: %v", err)
			return err
		}
		outFile, err = os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			log.Debugf("open file failed: %v", err)
			return err
		}
	}
	defer outFile.Close()

	rc, err := f.Open()
	if err != nil {
		log.Debugf("open file failed: %v", err)
		return err
	}
	defer rc.Close()

	if _, err = io.Copy(outFile, rc); err != nil {
		log.Debugf("copy failed: %v", err)
		return err
	}
	return nil
}

func isPathInDest(fpath, dest string) bool {
	rel, err := filepath.Rel(dest, fpath)
	if err != nil {
		return false
	}
	return filepath.IsLocal(rel)
}
