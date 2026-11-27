package main

import (
	"testing"
)

// TestIconFromICO 用真实嵌入的 brand.ico 检查 ICO 解析不会越界，
// 并确认能取到 16 点图标句柄。
func TestIconFromICO(t *testing.T) {
	if len(trayIconICO) < 6 {
		t.Fatal("embedded ico missing")
	}
	icon := iconFromICO(trayIconICO, 16)
	if icon == 0 {
		t.Fatal("iconFromICO returned no handle")
	}
	destroyIcon.Call(icon)
	if iconFromICO([]byte{0, 0, 1}, 16) != 0 {
		t.Fatal("truncated ico must yield no icon")
	}
}
