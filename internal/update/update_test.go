package update

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/bestruirui/octopus/internal/conf"
)

func releaseJSON(t *testing.T, tag string, draft, prerelease, withAsset bool) []byte {
	t.Helper()
	assets := []map[string]string{}
	if withAsset {
		filename, err := getDownloadFilename()
		if err != nil {
			t.Skip(err)
		}
		assets = append(assets, map[string]string{"name": filename})
	}
	body, err := json.Marshal(map[string]any{
		"tag_name": tag, "draft": draft, "prerelease": prerelease,
		"published_at": "2026-11-15T00:00:00Z", "body": "Fork release", "assets": assets,
	})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestLatestReleaseUsesSemanticVersionOrder(t *testing.T) {
	for _, test := range []struct {
		name, current, latest, status   string
		draft, prerelease, missingAsset bool
	}{
		{name: "numeric order", current: "v1.9.0", latest: "v1.10.0", status: "update_available"},
		{name: "same version", current: "v1.10.0", latest: "v1.10.0", status: "up_to_date"},
		{name: "older release", current: "v1.11.0", latest: "v1.10.0", status: "up_to_date"},
		{name: "build metadata", current: "v1.10.0+custom", latest: "v1.10.0", status: "up_to_date"},
		{name: "development", current: "dev", latest: "v1.10.0", status: "development"},
		{name: "unknown", current: "unknown", latest: "v1.10.0", status: "development"},
		{name: "preview installation", current: "v1.10.0-rc.1", latest: "v1.10.0", status: "development"},
		{name: "draft", current: "v1.0.0", latest: "v1.10.0", status: "no_release", draft: true},
		{name: "prerelease", current: "v1.0.0", latest: "v1.10.0-rc.1", status: "no_release", prerelease: true},
		{name: "missing archive", current: "v1.0.0", latest: "v1.10.0", status: "asset_missing", missingAsset: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := releaseJSON(t, test.latest, test.draft, test.prerelease, !test.missingAsset)
			info, err := getLatestInfo(test.current, func(endpoint string) ([]byte, error) {
				if endpoint != "https://api.github.com/repos/suprelory/octopus/releases/latest" {
					t.Fatalf("update queried the wrong repository: %s", endpoint)
				}
				return body, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if info.Status != test.status || info.UpdateAvailable != (test.status == "update_available") {
				t.Fatalf("unexpected update decision: %+v", info)
			}
		})
	}
}

func TestLatestReleaseHandlesHTTPFailures(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusForbidden, http.StatusTooManyRequests} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path != "/repos/suprelory/octopus/releases/latest" {
					t.Errorf("unexpected API path: %s", r.URL.Path)
				}
				w.WriteHeader(status)
				fmt.Fprint(w, `{"message":"GitHub API error"}`)
			}))
			defer server.Close()
			info, err := getLatestInfo("v1.0.0", func(endpoint string) ([]byte, error) {
				return doRequestWithFallback(strings.Replace(endpoint, "https://api.github.com", server.URL, 1))
			})
			if status == http.StatusNotFound {
				if err != nil || info.Status != "no_release" || info.UpdateAvailable || info.TagName != "" {
					t.Fatalf("missing release was not handled: info=%+v err=%v", info, err)
				}
			} else {
				var statusErr *httpStatusError
				if !errors.As(err, &statusErr) || statusErr.StatusCode != status {
					t.Fatalf("API failure was treated as a release: %v", err)
				}
			}
			if calls.Load() != 1 {
				t.Fatalf("unexpected retries for HTTP %d: %d", status, calls.Load())
			}
		})
	}
}

func TestLatestReleaseRejectsMalformedResponses(t *testing.T) {
	for _, body := range []string{`not JSON`, `{}`, `{"tag_name":"dev"}`, `{"tag_name":"v2.0.0-rc.1"}`} {
		_, err := getLatestInfo("v1.0.0", func(string) ([]byte, error) { return []byte(body), nil })
		if err == nil {
			t.Errorf("accepted malformed stable release: %s", body)
		}
	}
}

func TestDownloadPinsSelectedReleaseAndBuildRepository(t *testing.T) {
	previous := conf.Repository
	conf.Repository = "another-owner/octopus"
	t.Cleanup(func() { conf.Repository = previous })
	body := releaseJSON(t, "v1.2.0", false, false, true)
	request := func(endpoint string) ([]byte, error) {
		if endpoint != "https://api.github.com/repos/another-owner/octopus/releases/tags/v1.2.0" {
			t.Fatalf("selected version was not rechecked in the build repository: %s", endpoint)
		}
		return body, nil
	}
	downloadURL, err := resolveDownloadURL("v1.2.0", "v1.1.0", request)
	if err != nil {
		t.Fatal(err)
	}
	filename, _ := getDownloadFilename()
	if downloadURL != "https://github.com/another-owner/octopus/releases/download/v1.2.0/"+filename {
		t.Fatalf("download was not pinned to the selected fork release: %s", downloadURL)
	}
	info, err := getLatestInfo("v1.1.0", func(endpoint string) ([]byte, error) {
		if endpoint != "https://api.github.com/repos/another-owner/octopus/releases/latest" {
			t.Fatalf("latest query ignored build repository: %s", endpoint)
		}
		return body, nil
	})
	if err != nil || info.HTMLURL != "https://github.com/another-owner/octopus/releases/tag/v1.2.0" {
		t.Fatalf("release link ignored build repository: info=%+v err=%v", info, err)
	}
}

func TestDownloadRejectsUnsafeVersionTransitionsBeforeRequest(t *testing.T) {
	for _, test := range []struct{ current, target string }{
		{"v1.0.0", "v1.0.0"}, {"v2.0.0", "v1.0.0"}, {"dev", "v1.0.0"},
		{"unknown", "v1.0.0"}, {"v1.0.0-rc.1", "v1.0.0"}, {"v1.0.0", "v2.0.0-rc.1"},
		{"v1.0.0", ""}, {"v1.0.0", "../../other/release"},
	} {
		_, err := resolveDownloadURL(test.target, test.current, func(string) ([]byte, error) {
			t.Fatal("invalid version transition reached the network")
			return nil, nil
		})
		if !errors.Is(err, ErrUpdateUnavailable) {
			t.Errorf("%s -> %s was not rejected: %v", test.current, test.target, err)
		}
	}
}

func TestDownloadRechecksReleaseAndPlatformAsset(t *testing.T) {
	for _, test := range []struct {
		name, tag                    string
		draft, prerelease, withAsset bool
	}{
		{"changed tag", "v3.0.0", false, false, true},
		{"draft", "v2.0.0", true, false, true},
		{"prerelease", "v2.0.0", false, true, true},
		{"missing asset", "v2.0.0", false, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := releaseJSON(t, test.tag, test.draft, test.prerelease, test.withAsset)
			_, err := resolveDownloadURL("v2.0.0", "v1.0.0", func(string) ([]byte, error) { return body, nil })
			if !errors.Is(err, ErrUpdateUnavailable) {
				t.Fatalf("invalid release accepted: %v", err)
			}
		})
	}
}

func TestArchiveDownloadRejectsHTTPErrorBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, "not an archive")
	}))
	defer server.Close()
	data, err := doRequest(server.URL, false)
	var statusErr *httpStatusError
	if data != nil || !errors.As(err, &statusErr) || statusErr.StatusCode != http.StatusNotFound {
		t.Fatalf("error response would be passed to the archive extractor: %q, %v", data, err)
	}
}
