package main

import "testing"

func TestTrayLoadsBrandedResource(t *testing.T) {
	instance, _, _ := getModuleHandle.Call(0)
	if _, err := loadTrayIcon(instance); err != nil {
		t.Fatal(err)
	}
}
