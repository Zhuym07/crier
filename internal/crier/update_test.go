package crier

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Zhuym07/crier/internal/version"
)

func TestVersionOrdering(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
		err             bool
	}{
		{"0.1.4", "0.1.5", true, false},
		{"0.1.5", "0.1.5", false, false},
		{"0.1.6", "0.1.5", false, false},
		{"0.2.0", "0.10.0", true, false},
		{"v0.1.4", "v0.1.5", true, false},
		{"0.1.4-rc1", "0.1.5", true, false},
		{"dev", "0.1.5", false, true},
		{"0.1.4", "nightly", false, true},
	}
	for _, c := range cases {
		got, err := newerRelease(c.current, c.latest)
		if got != c.want || (err != nil) != c.err {
			t.Errorf("newerRelease(%q,%q)=%v,%v want %v err=%v", c.current, c.latest, got, err, c.want, c.err)
		}
	}
}

func TestCheckUpdateFindsNewerRelease(t *testing.T) {
	s := &Service{dir: t.TempDir()}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("missing User-Agent")
		}
		w.Write([]byte(`{"tag_name":"v9.9.9","html_url":"https://github.com/Zhuym07/crier/releases/tag/v9.9.9","assets":[{"name":"Crier-Setup-x64.exe","size":1,"browser_download_url":"https://example.invalid/setup.exe"}]}`))
	}))
	defer server.Close()
	old := latestReleaseURL
	latestReleaseURL = server.URL
	defer func() { latestReleaseURL = old }()
	version.Value = "0.1.4"
	info := s.CheckUpdate(context.Background())
	if info.Error != "" || !info.Available || info.Latest != "9.9.9" || info.Current != "0.1.4" || info.PageURL == "" {
		t.Fatalf("unexpected update info: %+v", info)
	}
}

func TestCheckUpdateReportsErrors(t *testing.T) {
	s := &Service{dir: t.TempDir()}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()
	old := latestReleaseURL
	latestReleaseURL = server.URL
	defer func() { latestReleaseURL = old }()
	info := s.CheckUpdate(context.Background())
	if info.Error == "" || info.Available {
		t.Fatalf("error must be reported without claiming an update: %+v", info)
	}
}

func TestLaunchInstallerRejectsForeignPath(t *testing.T) {
	s := &Service{dir: t.TempDir()}
	if err := s.LaunchInstaller("C:\\Windows\\notepad.exe"); err == nil {
		t.Fatal("must refuse executables outside the update directory")
	}
}
