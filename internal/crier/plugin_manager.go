package crier

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// OfficialPluginReleaseURL 是官方安装包与独立插件程序的发布页。
// 应用只把它交给系统浏览器打开，绝不自行下载或执行远程内容。
const OfficialPluginReleaseURL = "https://github.com/Zhuym07/crier/releases/latest"

// 外部插件 ID：小写字母或数字开头，只允许小写字母、数字与 . _ -。
var pluginIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// 内置插件由 Config 驱动，plugins.json 里不允许出现它们的 ID。
var builtinPluginNames = map[string]string{
	"email":    "邮箱通道",
	"telegram": "Telegram 打印",
}

// pluginRegistryMu 串行化 plugins.json 的读改写，避免界面上的操作互相覆盖。
var pluginRegistryMu sync.Mutex

// PluginSpec 是界面提交给 SavePlugin 的外部插件定义。
// Settings 会原样作为方法参数交给插件，必须是 JSON 对象。
type PluginSpec struct {
	ID         string          `json:"id"`
	Executable string          `json:"executable"`
	Settings   json.RawMessage `json:"settings"`
	AllowAI    bool            `json:"allowAI"`
	AllowRaw   bool            `json:"allowRaw"`
}

// PluginInfo 是插件页使用的只读视图。内置插件只提供 ID 与启用状态，
// 具体的地址、账号等仍由 Config 管理。
type PluginInfo struct {
	ID         string          `json:"id"`
	Executable string          `json:"executable"`
	Settings   json.RawMessage `json:"settings,omitempty"`
	AllowAI    bool            `json:"allowAI"`
	AllowRaw   bool            `json:"allowRaw"`
	Builtin    bool            `json:"builtin"`
	Disabled   bool            `json:"disabled"`
	Enabled    bool            `json:"enabled"`
	Exists     bool            `json:"exists"`
}

func pluginRegistryPath(dir string) string { return filepath.Join(dir, "plugins.json") }

// ListPlugins 返回内置通道与 plugins.json 里的外部插件。
// 内置插件的启用状态来自 Config，外部插件来自 Disabled 字段。
func (s *Service) ListPlugins() ([]PluginInfo, error) {
	cfg := s.Config()
	out := []PluginInfo{
		{ID: "email", Builtin: true, Enabled: cfg.Email.Enabled},
		{ID: "telegram", Builtin: true, Enabled: cfg.Telegram.Enabled},
	}
	list, err := s.loadPluginRegistry()
	if err != nil {
		return nil, err
	}
	for _, p := range list {
		out = append(out, pluginInfo(p))
	}
	return out, nil
}

// SavePlugin 登记（或更新）一个外部插件。新导入的插件默认停用，需要用户在
// 插件页明确启用后才会参与收取；同名 ID 更新时保留原有的启用状态，
// 以便用户下载新版本后直接覆盖程序路径。启用/停用请走 SetPluginEnabled。
func (s *Service) SavePlugin(spec PluginSpec) (PluginInfo, error) {
	pluginRegistryMu.Lock()
	defer pluginRegistryMu.Unlock()

	id := strings.TrimSpace(spec.ID)
	if err := validatePluginID(id); err != nil {
		return PluginInfo{}, err
	}
	exe, err := validatePluginExecutable(spec.Executable)
	if err != nil {
		return PluginInfo{}, err
	}
	settings := normalizePluginSettings(spec.Settings)
	if err := validatePluginSettings(settings); err != nil {
		return PluginInfo{}, err
	}
	list, err := s.readPluginRegistry()
	if err != nil {
		return PluginInfo{}, err
	}

	var saved *plugin
	for i := range list {
		if list[i].ID != id {
			continue
		}
		list[i].Executable = exe
		list[i].Settings = settings
		list[i].AllowAI = spec.AllowAI
		list[i].AllowRaw = spec.AllowRaw
		saved = &list[i]
		break
	}
	if saved == nil {
		list = append(list, plugin{
			ID:         id,
			Executable: exe,
			Settings:   settings,
			AllowAI:    spec.AllowAI,
			AllowRaw:   spec.AllowRaw,
			Disabled:   true, // 新导入默认停用，等用户确认后手动启用
		})
		saved = &list[len(list)-1]
	}
	if err := validatePluginRegistry(list); err != nil {
		return PluginInfo{}, err
	}
	if err := writeJSON(pluginRegistryPath(s.dir), list); err != nil {
		return PluginInfo{}, err
	}
	return pluginInfo(*saved), nil
}

// SetPluginEnabled 启用或停用外部插件。内置插件没有可写的开关，必须走 Config。
func (s *Service) SetPluginEnabled(id string, enabled bool) error {
	id = strings.TrimSpace(id)
	if _, ok := builtinPluginNames[id]; ok {
		return errors.New("内置插件请在插件卡片中启用后保存")
	}
	pluginRegistryMu.Lock()
	defer pluginRegistryMu.Unlock()
	list, err := s.readPluginRegistry()
	if err != nil {
		return err
	}
	for i := range list {
		if list[i].ID != id {
			continue
		}
		if list[i].Disabled == !enabled {
			return nil
		}
		list[i].Disabled = !enabled
		return writeJSON(pluginRegistryPath(s.dir), list)
	}
	return fmt.Errorf("未找到插件 %s", id)
}

// RemovePlugin 从 plugins.json 移除外部插件。历史游标留在队列里，
// 同名插件再次导入时不会重复打印旧事件。
func (s *Service) RemovePlugin(id string) error {
	id = strings.TrimSpace(id)
	if _, ok := builtinPluginNames[id]; ok {
		return errors.New("内置插件不能移除，只能停用")
	}
	pluginRegistryMu.Lock()
	defer pluginRegistryMu.Unlock()
	list, err := s.readPluginRegistry()
	if err != nil {
		return err
	}
	kept := make([]plugin, 0, len(list))
	found := false
	for _, p := range list {
		if p.ID == id {
			found = true
			continue
		}
		kept = append(kept, p)
	}
	if !found {
		return fmt.Errorf("未找到插件 %s", id)
	}
	return writeJSON(pluginRegistryPath(s.dir), kept)
}

// loadPluginRegistry 读取并校验外部插件列表。插件页与轮询逻辑都用它，
// 保证界面看到的条目和真正会被调起的条目来自同一份校验结果。
func (s *Service) loadPluginRegistry() ([]plugin, error) {
	pluginRegistryMu.Lock()
	defer pluginRegistryMu.Unlock()
	return s.readPluginRegistry()
}

// readPluginRegistry 读取并校验 plugins.json。调用方需持有 pluginRegistryMu。
// 文件不存在视为空列表；内容损坏时报错，绝不静默覆盖用户文件。
func (s *Service) readPluginRegistry() ([]plugin, error) {
	path := pluginRegistryPath(s.dir)
	var list []plugin
	err := readJSON(path, &list)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取插件配置 %s 失败: %w", path, err)
	}
	if err := validatePluginRegistry(list); err != nil {
		return nil, fmt.Errorf("插件配置 %s 无效: %w", path, err)
	}
	return list, nil
}

func pluginInfo(p plugin) PluginInfo {
	return PluginInfo{
		ID:         p.ID,
		Executable: p.Executable,
		Settings:   p.Settings,
		AllowAI:    p.AllowAI,
		AllowRaw:   p.AllowRaw,
		Disabled:   p.Disabled,
		Enabled:    !p.Disabled,
		Exists:     pluginFileExists(p.Executable),
	}
}

func pluginFileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func validatePluginRegistry(list []plugin) error {
	ids := make(map[string]bool, len(list))
	executables := make(map[string]string, len(list))
	for i := range list {
		p := &list[i]
		if err := validatePluginID(p.ID); err != nil {
			return err
		}
		if ids[p.ID] {
			return fmt.Errorf("插件 ID 重复: %s", p.ID)
		}
		ids[p.ID] = true
		if !filepath.IsAbs(p.Executable) {
			return fmt.Errorf("插件 %s 的程序路径必须是绝对路径", p.ID)
		}
		exe := filepath.Clean(p.Executable)
		p.Executable = exe
		key := strings.ToLower(exe)
		if other, ok := executables[key]; ok {
			return fmt.Errorf("插件 %s 与 %s 使用了同一个程序", p.ID, other)
		}
		executables[key] = p.ID
		if err := validatePluginSettings(p.Settings); err != nil {
			return fmt.Errorf("插件 %s: %w", p.ID, err)
		}
	}
	return nil
}

func validatePluginID(id string) error {
	if id == "" {
		return errors.New("插件 ID 不能为空")
	}
	if name, ok := builtinPluginNames[id]; ok {
		return fmt.Errorf("%s 是内置插件，无需导入", name)
	}
	if !pluginIDPattern.MatchString(id) {
		return errors.New("插件 ID 只能由小写字母、数字和 . _ - 组成，以字母或数字开头，最长 64 个字符")
	}
	return nil
}

// validatePluginExecutable 校验用户在文件选择器中明确选中的本地程序。
// 只接受绝对路径的 .exe 普通文件，且不能是传令自己。
func validatePluginExecutable(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("请选择插件程序")
	}
	if !filepath.IsAbs(path) {
		return "", errors.New("插件程序必须使用绝对路径")
	}
	path = filepath.Clean(path)
	if !strings.EqualFold(filepath.Ext(path), ".exe") {
		return "", errors.New("插件程序必须是 .exe 文件")
	}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", errors.New("插件程序不存在，请重新选择")
	}
	if err != nil {
		return "", fmt.Errorf("无法读取插件程序: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("插件程序必须是普通文件")
	}
	if self, err := os.Executable(); err == nil {
		if selfPath, err := filepath.Abs(self); err == nil && strings.EqualFold(filepath.Clean(selfPath), path) {
			return "", errors.New("不能把传令自身登记为插件")
		}
	}
	return path, nil
}

func normalizePluginSettings(raw json.RawMessage) json.RawMessage {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return json.RawMessage("{}")
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, trimmed); err != nil {
		return raw
	}
	return json.RawMessage(compact.Bytes())
}

func validatePluginSettings(raw json.RawMessage) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	if trimmed[0] != '{' {
		return errors.New("插件设置必须是 JSON 对象")
	}
	var params map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &params); err != nil {
		return errors.New("插件设置必须是合法的 JSON 对象")
	}
	return nil
}
