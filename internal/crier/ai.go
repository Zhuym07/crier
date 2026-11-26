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
	u, err := url.Parse(c.URL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || c.Model == "" {
		return errors.New("请填写 HTTP 或 HTTPS 接口及模型名称")
	}
	return nil
}

func askAI(ctx context.Context, c AIConfig, instruction, content string) (string, error) {
	if err := validateAI(c); err != nil {
		return "", err
	}
	payload, err := json.Marshal(map[string]any{"model": c.Model, "messages": []map[string]string{{"role": "system", "content": instruction}, {"role": "user", "content": content}}})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(payload))
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
