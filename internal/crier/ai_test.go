package crier

import "testing"

func TestValidateAIAllowsHTTPAndHTTPS(t *testing.T) {
	for _, endpoint := range []string{"http://127.0.0.1:8080/v1/chat/completions", "https://api.example.com/v1/chat/completions"} {
		if err := validateAI(AIConfig{URL: endpoint, Model: "chat"}); err != nil {
			t.Errorf("%s: %v", endpoint, err)
		}
	}
	for _, endpoint := range []string{"ftp://api.example.com/chat", "//api.example.com/chat", "https:///path"} {
		if err := validateAI(AIConfig{URL: endpoint, Model: "chat"}); err == nil {
			t.Errorf("accepted %s", endpoint)
		}
	}
}
