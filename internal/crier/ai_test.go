package crier

import "testing"

func TestAIEndpointAcceptsBaseURL(t *testing.T) {
	cases := map[string]string{
		"https://integrate.api.nvidia.com/v1":        "https://integrate.api.nvidia.com/v1/chat/completions",
		"https://integrate.api.nvidia.com/v1/":       "https://integrate.api.nvidia.com/v1/chat/completions",
		"https://api.deepseek.com":                   "https://api.deepseek.com/chat/completions",
		"https://api.openai.com/v1/chat/completions": "https://api.openai.com/v1/chat/completions",
		"  https://openrouter.ai/api/v1  ":           "https://openrouter.ai/api/v1/chat/completions",
		"http://127.0.0.1:8080/v1":                   "http://127.0.0.1:8080/v1/chat/completions",
	}
	for input, want := range cases {
		got, err := aiEndpoint(input)
		if err != nil || got != want {
			t.Errorf("aiEndpoint(%q)=%q,%v want %q", input, got, err, want)
		}
	}
	for _, bad := range []string{"", "integrate.api.nvidia.com/v1", "ftp://host/v1", "https://host/v1?x=1"} {
		if _, err := aiEndpoint(bad); err == nil {
			t.Errorf("accepted invalid Base URL %q", bad)
		}
	}
}

func TestValidateAIAllowsHTTPAndHTTPS(t *testing.T) {
	for _, endpoint := range []string{"http://127.0.0.1:8080/v1", "https://api.example.com/v1"} {
		if err := validateAI(AIConfig{URL: endpoint, Model: "chat"}); err != nil {
			t.Errorf("%s: %v", endpoint, err)
		}
	}
	if err := validateAI(AIConfig{URL: "https://api.example.com/v1"}); err == nil {
		t.Error("missing model must fail")
	}
	for _, endpoint := range []string{"ftp://api.example.com/chat", "//api.example.com/chat", "https:///path"} {
		if err := validateAI(AIConfig{URL: endpoint, Model: "chat"}); err == nil {
			t.Errorf("accepted %s", endpoint)
		}
	}
}
