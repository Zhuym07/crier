package crier

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Zhuym07/crier/internal/version"
)

// releasesPage 是公开的发布页，人工下载安装包时使用。
const releasesPage = "https://github.com/Zhuym07/crier/releases/latest"

// ReleasesPage 对外开放同一地址，界面跳转使用。
const ReleasesPage = releasesPage

const (
	checkTimeout = 15 * time.Second
)

// latestReleaseURL 指向 GitHub 的 latest release 接口，测试中可替换。
var latestReleaseURL = "https://api.github.com/repos/Zhuym07/crier/releases/latest"

// UpdateInfo 是一次检查更新的结果。只有确认有新版本时才把 Available 设为 true，
// 检查失败通过 Error 返回，不阻断正常使用。
type UpdateInfo struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	Available bool   `json:"available"`
	PageURL   string `json:"pageURL"`
	Error     string `json:"error,omitempty"`
}

type releaseAsset struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	URL  string `json:"browser_download_url"`
}

type releaseInfo struct {
	TagName string         `json:"tag_name"`
	HTMLURL string         `json:"html_url"`
	Assets  []releaseAsset `json:"assets"`
}

func (r releaseInfo) installer() (releaseAsset, error) {
	for _, asset := range r.Assets {
		if asset.Name == "Crier-Setup-x64.exe" {
			return asset, nil
		}
	}
	return releaseAsset{}, errors.New("发布页缺少安装包")
}

func (r releaseInfo) tag() string { return strings.TrimPrefix(strings.TrimSpace(r.TagName), "v") }

func (r releaseInfo) sums() (releaseAsset, bool) {
	for _, asset := range r.Assets {
		if asset.Name == "SHA256SUMS" {
			return asset, true
		}
	}
	return releaseAsset{}, false
}

func (s *Service) CurrentVersion() string { return version.Value }

// InstalledInstallerPath 返回安装版的卸载程序，用来判断当前是否是安装版。
func (s *Service) InstalledInstallerPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	path := filepath.Join(filepath.Dir(exe), "unins000.exe")
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
		return "", errors.New("当前不是安装版，无法自动运行安装包")
	}
	return path, nil
}

func (s *Service) CheckUpdate(ctx context.Context) UpdateInfo {
	info := UpdateInfo{Current: version.Value, PageURL: ReleasesPage}
	release, err := fetchLatestRelease(ctx)
	if err != nil {
		info.Error = err.Error()
		return info
	}
	info.Latest, info.PageURL = release.tag(), release.HTMLURL
	if info.PageURL == "" {
		info.PageURL = ReleasesPage
	}
	fresh, err := newerRelease(version.Value, release.tag())
	if err != nil {
		info.Error = err.Error()
		return info
	}
	info.Available = fresh
	return info
}

func fetchLatestRelease(ctx context.Context) (releaseInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseURL, nil)
	if err != nil {
		return releaseInfo{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Crier/"+version.Value)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return releaseInfo{}, errors.New("无法连接 GitHub 检查更新")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return releaseInfo{}, fmt.Errorf("检查更新失败：GitHub 返回 HTTP %d", resp.StatusCode)
	}
	var release releaseInfo
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&release); err != nil {
		return releaseInfo{}, errors.New("发布信息格式无法识别")
	}
	return release, nil
}

// newerRelease 判断发布标签是否高于当前版本。
// dev 等无法解析的当前版本不提示更新，避免开发版误报。
func newerRelease(current, latest string) (bool, error) {
	if current == "" || current == "dev" {
		return false, errors.New("开发版不检查更新，请使用安装版")
	}
	have, ok := parseVersion(current)
	if !ok {
		return false, errors.New("当前版本号无法识别")
	}
	next, ok := parseVersion(latest)
	if !ok {
		return false, errors.New("发布标签无法识别")
	}
	return compareVersions(have, next) < 0, nil
}

func parseVersion(text string) ([3]int, bool) {
	var parsed [3]int
	text = strings.TrimPrefix(strings.TrimSpace(text), "v")
	if core, _, found := strings.Cut(text, "-"); found {
		text = core
	}
	parts := strings.Split(text, ".")
	if len(parts) != 3 {
		return parsed, false
	}
	for i, part := range parts {
		if part == "" {
			return parsed, false
		}
		value := 0
		for _, digit := range part {
			if digit < '0' || digit > '9' {
				return parsed, false
			}
			value = value*10 + int(digit-'0')
		}
		parsed[i] = value
	}
	return parsed, true
}

func compareVersions(a, b [3]int) int {
	for i := range a {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

// DownloadUpdate 下载新版安装包，核对下载大小与 SHA-256 后才返回本地路径。
func (s *Service) DownloadUpdate(ctx context.Context) (string, error) {
	release, err := fetchLatestRelease(ctx)
	if err != nil {
		return "", err
	}
	fresh, err := newerRelease(version.Value, release.tag())
	if err != nil {
		return "", err
	}
	if !fresh {
		return "", errors.New("已经是最新版本")
	}
	asset, err := release.installer()
	if err != nil {
		return "", err
	}
	sums, ok := release.sums()
	if !ok {
		return "", errors.New("发布信息缺少校验清单 SHA256SUMS")
	}
	expected, err := findChecksum(ctx, sums.URL, asset.Name)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Crier/"+version.Value)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", errors.New("下载安装包失败")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载失败：HTTP %d", resp.StatusCode)
	}
	dir := filepath.Join(s.dir, "updates")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	temp, err := os.CreateTemp(dir, "download-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(temp.Name())
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(temp, hash), io.LimitReader(resp.Body, 512<<20))
	if err != nil {
		temp.Close()
		return "", errors.New("下载中断")
	}
	if err := temp.Close(); err != nil {
		return "", err
	}
	if written != asset.Size {
		return "", errors.New("安装包大小与发布信息不符")
	}
	if fmt.Sprintf("%x", hash.Sum(nil)) != expected {
		return "", errors.New("安装包校验失败，已丢弃下载内容")
	}
	final := filepath.Join(dir, "Crier-Setup-"+release.tag()+".exe")
	return final, os.Rename(temp.Name(), final)
}

func findChecksum(ctx context.Context, url, name string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", errors.New("无法读取校验清单")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", errors.New("无法读取校验清单")
	}
	scanner := bufio.NewScanner(io.LimitReader(resp.Body, 1<<20))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name {
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", errors.New("校验清单中没有安装包")
}

// LaunchInstaller 启动已下载的安装包。路径必须位于本程序的更新目录内。
func (s *Service) LaunchInstaller(path string) error {
	updates := filepath.Join(s.dir, "updates")
	rel, err := filepath.Rel(updates, filepath.Clean(path))
	if err != nil || strings.HasPrefix(rel, "..") || !strings.HasSuffix(strings.ToLower(rel), ".exe") {
		return errors.New("只能运行本程序下载的安装包")
	}
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
		return errors.New("安装包不存在")
	}
	if err := exec.Command(path).Start(); err != nil {
		return fmt.Errorf("无法启动安装包: %w", err)
	}
	return nil
}
