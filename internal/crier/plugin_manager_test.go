package crier

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newPluginTestService(t *testing.T) *Service {
	t.Helper()
	return &Service{dir: t.TempDir(), queue: queue{Cursors: make(map[string]string)}}
}

// stubExecutable 造一个够用的 .exe 文件：管理器只校验存在性、类型与后缀，
// 不会真的运行它。
func stubExecutable(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("MZ stub"), 0600); err != nil {
		t.Fatalf("write stub exe: %v", err)
	}
	return path
}

func storedPlugins(t *testing.T, s *Service) []plugin {
	t.Helper()
	var list []plugin
	if err := readJSON(pluginRegistryPath(s.dir), &list); err != nil {
		t.Fatalf("read plugins.json: %v", err)
	}
	return list
}

func TestListPluginsIncludesBuiltins(t *testing.T) {
	s := newPluginTestService(t)
	s.config.Email.Enabled = true

	list, err := s.ListPlugins()
	if err != nil {
		t.Fatalf("ListPlugins: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("want 2 builtins, got %d: %+v", len(list), list)
	}
	if list[0].ID != "email" || !list[0].Builtin || !list[0].Enabled {
		t.Fatalf("email builtin wrong: %+v", list[0])
	}
	if list[1].ID != "telegram" || !list[1].Builtin || list[1].Enabled {
		t.Fatalf("telegram builtin wrong: %+v", list[1])
	}
}

func TestSavePluginAddsExternal(t *testing.T) {
	s := newPluginTestService(t)
	exe := stubExecutable(t, t.TempDir(), "demo-source.exe")

	info, err := s.SavePlugin(PluginSpec{
		ID:         "demo-source",
		Executable: exe,
		Settings:   json.RawMessage(`{"token": "abc", "limit": 3}`),
		AllowAI:    true,
	})
	if err != nil {
		t.Fatalf("SavePlugin: %v", err)
	}
	if info.Builtin || !info.Disabled || info.Enabled || !info.Exists {
		t.Fatalf("new import must be disabled until explicitly enabled: %+v", info)
	}
	var got map[string]any
	if err := json.Unmarshal(info.Settings, &got); err != nil || got["token"] != "abc" || got["limit"] != float64(3) {
		t.Fatalf("settings not preserved: %s (err %v)", info.Settings, err)
	}

	stored := storedPlugins(t, s)
	if len(stored) != 1 || stored[0].ID != "demo-source" || stored[0].Executable != exe || !stored[0].AllowAI {
		t.Fatalf("stored registry wrong: %+v", stored)
	}
	if !stored[0].Disabled {
		t.Fatalf("new plugin must be stored disabled")
	}

	list, err := s.ListPlugins()
	if err != nil {
		t.Fatalf("ListPlugins: %v", err)
	}
	if len(list) != 3 || list[2].ID != "demo-source" || list[2].Enabled {
		t.Fatalf("external plugin missing or wrongly enabled: %+v", list)
	}

	if err := s.SetPluginEnabled("demo-source", true); err != nil {
		t.Fatalf("enable imported plugin: %v", err)
	}
	if stored := storedPlugins(t, s); stored[0].Disabled {
		t.Fatalf("explicit enable not persisted: %+v", stored)
	}
}

func TestSavePluginNormalizesEmptySettings(t *testing.T) {
	s := newPluginTestService(t)
	exe := stubExecutable(t, t.TempDir(), "plain.exe")
	for _, raw := range []json.RawMessage{nil, json.RawMessage("  "), json.RawMessage("null")} {
		if _, err := s.SavePlugin(PluginSpec{ID: "plain", Executable: exe, Settings: raw}); err != nil {
			t.Fatalf("SavePlugin(%q): %v", raw, err)
		}
		stored := storedPlugins(t, s)
		if string(stored[0].Settings) != "{}" {
			t.Fatalf("settings %q normalized to %s, want {}", raw, stored[0].Settings)
		}
	}
}

func TestSavePluginUpdatesExistingAndKeepsDisabled(t *testing.T) {
	s := newPluginTestService(t)
	dir := t.TempDir()
	first := stubExecutable(t, dir, "v1.exe")
	second := stubExecutable(t, dir, "v2.exe")

	if _, err := s.SavePlugin(PluginSpec{ID: "demo", Executable: first, Settings: json.RawMessage(`{"v":1}`)}); err != nil {
		t.Fatalf("save v1: %v", err)
	}
	if err := s.SetPluginEnabled("demo", false); err != nil {
		t.Fatalf("disable: %v", err)
	}

	info, err := s.SavePlugin(PluginSpec{ID: "demo", Executable: second, Settings: json.RawMessage(`{"v":2}`), AllowRaw: true})
	if err != nil {
		t.Fatalf("save v2: %v", err)
	}
	if info.Enabled || !info.Disabled {
		t.Fatalf("update must keep plugin disabled: %+v", info)
	}
	if info.Executable != second || !info.AllowRaw {
		t.Fatalf("update did not relink executable/flags: %+v", info)
	}
	stored := storedPlugins(t, s)
	if len(stored) != 1 {
		t.Fatalf("update must not duplicate entries: %+v", stored)
	}
}

func TestSavePluginRejectsInvalidSpecs(t *testing.T) {
	s := newPluginTestService(t)
	dir := t.TempDir()
	good := stubExecutable(t, dir, "good.exe")
	other := stubExecutable(t, dir, "other.exe")
	notExe := filepath.Join(dir, "plugin.dll")
	if err := os.WriteFile(notExe, []byte("MZ"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SavePlugin(PluginSpec{ID: "taken", Executable: other}); err != nil {
		t.Fatalf("seed plugin: %v", err)
	}

	self, selfErr := os.Executable()
	selfCase := PluginSpec{ID: "self", Executable: self, Settings: json.RawMessage(`{}`)}
	if selfErr != nil || !strings.EqualFold(filepath.Ext(self), ".exe") {
		selfCase = PluginSpec{} // 无法构造自引用场景时跳过该行
	}

	cases := []struct {
		name string
		spec PluginSpec
		want string
	}{
		{"empty id", PluginSpec{Executable: good}, "插件 ID 不能为空"},
		{"uppercase id", PluginSpec{ID: "Demo", Executable: good}, "插件 ID 只能"},
		{"builtin id", PluginSpec{ID: "email", Executable: good}, "内置插件"},
		{"relative exe", PluginSpec{ID: "rel", Executable: `build\rel.exe`}, "绝对路径"},
		{"not exe", PluginSpec{ID: "dll", Executable: notExe}, ".exe"},
		{"missing exe", PluginSpec{ID: "gone", Executable: filepath.Join(dir, "gone.exe")}, "不存在"},
		{"missing exe path", PluginSpec{ID: "blank"}, "请选择插件程序"},
		{"settings array", PluginSpec{ID: "arr", Executable: good, Settings: json.RawMessage(`[]`)}, "JSON 对象"},
		{"settings string", PluginSpec{ID: "str", Executable: good, Settings: json.RawMessage(`"hi"`)}, "JSON 对象"},
		{"settings broken", PluginSpec{ID: "broken", Executable: good, Settings: json.RawMessage(`{oops`)}, "合法的 JSON"},
		{"duplicate executable", PluginSpec{ID: "copy", Executable: other}, "同一个程序"},
	}
	if selfCase.Executable != "" {
		cases = append(cases, struct {
			name string
			spec PluginSpec
			want string
		}{"self executable", selfCase, "传令自身"})
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := s.SavePlugin(tc.spec); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("SavePlugin error = %v, want substring %q", err, tc.want)
			}
		})
	}
	if stored := storedPlugins(t, s); len(stored) != 1 || stored[0].ID != "taken" {
		t.Fatalf("failed saves must not change registry: %+v", stored)
	}
}

func TestSetPluginEnabled(t *testing.T) {
	s := newPluginTestService(t)
	exe := stubExecutable(t, t.TempDir(), "toggle.exe")
	if _, err := s.SavePlugin(PluginSpec{ID: "toggle", Executable: exe}); err != nil {
		t.Fatalf("save: %v", err)
	}

	if err := s.SetPluginEnabled("toggle", false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	list, err := s.ListPlugins()
	if err != nil {
		t.Fatalf("ListPlugins: %v", err)
	}
	if list[2].Enabled || !list[2].Disabled {
		t.Fatalf("plugin should be disabled: %+v", list[2])
	}
	if stored := storedPlugins(t, s); !stored[0].Disabled {
		t.Fatalf("disabled flag not persisted: %+v", stored)
	}

	if err := s.SetPluginEnabled("toggle", true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if stored := storedPlugins(t, s); stored[0].Disabled {
		t.Fatalf("enabled flag not persisted: %+v", stored)
	}

	if err := s.SetPluginEnabled("missing", false); err == nil {
		t.Fatal("SetPluginEnabled unknown id should fail")
	}
	if err := s.SetPluginEnabled("email", false); err == nil {
		t.Fatal("SetPluginEnabled builtin should fail")
	}
}

func TestRemovePlugin(t *testing.T) {
	s := newPluginTestService(t)
	dir := t.TempDir()
	keep := stubExecutable(t, dir, "keep.exe")
	drop := stubExecutable(t, dir, "drop.exe")
	if _, err := s.SavePlugin(PluginSpec{ID: "keep", Executable: keep}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SavePlugin(PluginSpec{ID: "drop", Executable: drop}); err != nil {
		t.Fatal(err)
	}

	if err := s.RemovePlugin("drop"); err != nil {
		t.Fatalf("RemovePlugin: %v", err)
	}
	if stored := storedPlugins(t, s); len(stored) != 1 || stored[0].ID != "keep" {
		t.Fatalf("remove did not persist: %+v", stored)
	}
	if err := s.RemovePlugin("drop"); err == nil {
		t.Fatal("removing a missing plugin should fail")
	}
	if err := s.RemovePlugin("email"); err == nil {
		t.Fatal("removing a builtin plugin should fail")
	}
	if err := s.RemovePlugin("keep"); err != nil {
		t.Fatalf("remove last: %v", err)
	}
	if stored := storedPlugins(t, s); len(stored) != 0 {
		t.Fatalf("last removal should leave an empty registry: %+v", stored)
	}
}

func TestListPluginsReportsMissingExecutable(t *testing.T) {
	s := newPluginTestService(t)
	dir := t.TempDir()
	exe := stubExecutable(t, dir, "gone.exe")
	if _, err := s.SavePlugin(PluginSpec{ID: "gone", Executable: exe}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(exe); err != nil {
		t.Fatal(err)
	}

	list, err := s.ListPlugins()
	if err != nil {
		t.Fatalf("ListPlugins must tolerate missing files: %v", err)
	}
	if list[2].ID != "gone" || list[2].Exists {
		t.Fatalf("missing executable not reported: %+v", list[2])
	}
}

func TestPluginRegistryRejectsBrokenFiles(t *testing.T) {
	dir := t.TempDir()
	exe := stubExecutable(t, dir, "ok.exe")
	exe2 := stubExecutable(t, dir, "ok2.exe")

	cases := []struct {
		name string
		raw  any
		want string
	}{
		{"duplicate id", []plugin{
			{ID: "dup", Executable: exe},
			{ID: "dup", Executable: exe2},
		}, "重复"},
		{"builtin id", []plugin{{ID: "telegram", Executable: exe}}, "内置插件"},
		{"relative executable", []plugin{{ID: "rel", Executable: `build\rel.exe`}}, "绝对路径"},
		{"duplicate executable", []plugin{
			{ID: "one", Executable: exe},
			{ID: "two", Executable: exe},
		}, "同一个程序"},
		{"non-object settings", []plugin{{ID: "arr", Executable: exe, Settings: json.RawMessage(`[]`)}}, "JSON 对象"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &Service{dir: dir, queue: queue{Cursors: make(map[string]string)}}
			if err := writeJSON(pluginRegistryPath(dir), tc.raw); err != nil {
				t.Fatal(err)
			}
			if _, err := s.ListPlugins(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ListPlugins error = %v, want substring %q", err, tc.want)
			}
			if _, err := s.SavePlugin(PluginSpec{ID: "new", Executable: exe2}); err == nil {
				t.Fatal("SavePlugin must refuse to overwrite a broken registry")
			}
			var list []plugin
			if err := readJSON(pluginRegistryPath(dir), &list); err != nil {
				t.Fatalf("broken registry must be left untouched: %v", err)
			}
		})
	}

	t.Run("invalid json", func(t *testing.T) {
		s := &Service{dir: dir, queue: queue{Cursors: make(map[string]string)}}
		path := pluginRegistryPath(dir)
		if err := os.WriteFile(path, []byte("{not json"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := s.ListPlugins(); err == nil || !strings.Contains(err.Error(), "读取插件配置") {
			t.Fatalf("ListPlugins error = %v, want read failure", err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "{not json" {
			t.Fatalf("broken file was rewritten: %s", data)
		}
	})
}

func TestSavedPluginsJoinServiceSources(t *testing.T) {
	s := newPluginTestService(t)
	exe := stubExecutable(t, t.TempDir(), "source.exe")
	if _, err := s.SavePlugin(PluginSpec{ID: "source", Executable: exe}); err != nil {
		t.Fatal(err)
	}

	sources, err := s.plugins(Config{})
	if err != nil {
		t.Fatalf("plugins: %v", err)
	}
	if len(sources) != 0 {
		t.Fatalf("new import must not be polled before enabling: %+v", sources)
	}

	if err := s.SetPluginEnabled("source", true); err != nil {
		t.Fatal(err)
	}
	sources, err = s.plugins(Config{})
	if err != nil {
		t.Fatalf("plugins after enable: %v", err)
	}
	if len(sources) != 1 || sources[0].ID != "source" {
		t.Fatalf("saved plugin not polled after enabling: %+v", sources)
	}

	if err := s.SetPluginEnabled("source", false); err != nil {
		t.Fatal(err)
	}
	sources, err = s.plugins(Config{})
	if err != nil {
		t.Fatalf("plugins after disable: %v", err)
	}
	if len(sources) != 0 {
		t.Fatalf("disabled plugin must not be polled: %+v", sources)
	}
}
