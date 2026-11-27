package crier

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

func validateAI(c AIConfig) error {
	_, err := aiEndpoint(c.URL)
	if err != nil {
		return err
	}
	if strings.TrimSpace(c.Model) == "" {
		return errors.New("请填写模型名称")
	}
	return nil
}

// aiEndpoint 把用户填写的 Base URL 补全成 OpenAI 兼容的聊天补全地址。
// 主流服务商给出的都是 https://host/v1 这种形式，这里按行业惯例补后缀；
// 已经带 /chat/completions 的完整地址会原样使用，兼容旧配置。
func aiEndpoint(raw string) (string, error) {
	base := strings.TrimSpace(raw)
	if base == "" {
		return "", errors.New("请填写 Base URL")
	}
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("Base URL 需要是 http(s):// 开头的地址，例如 https://integrate.api.nvidia.com/v1")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	if !strings.HasSuffix(u.Path, "/chat/completions") {
		u.Path += "/chat/completions"
	}
	return u.String(), nil
}

func askAI(ctx context.Context, c AIConfig, instruction, content string) (string, error) {
	endpoint, err := aiEndpoint(c.URL)
	if err != nil {
		return "", err
	}
	if err := validateAI(c); err != nil {
		return "", err
	}
	payload, err := json.Marshal(map[string]any{"model": c.Model, "messages": []map[string]string{{"role": "system", "content": instruction}, {"role": "user", "content": content}}})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", errors.New("invalid AI endpoint")
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Key != "" {
		req.Header.Set("Authorization", "Bearer "+c.Key)
	}
	client := http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return "", errors.New("AI 请求失败，请检查网络和接口地址")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("AI 接口返回 HTTP %d", resp.StatusCode)
	}
	var answer struct {
		Choices []struct{ Message struct{ Content string } }
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&answer); err != nil {
		return "", errors.New("无法解析 AI 回复")
	}
	if len(answer.Choices) == 0 || strings.TrimSpace(answer.Choices[0].Message.Content) == "" {
		return "", errors.New("AI 回复为空")
	}
	return strings.TrimSpace(answer.Choices[0].Message.Content), nil
}
