package crier

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Config struct {
	Printer   string      `json:"printer"`
	Email     EmailConfig `json:"email"`
	AI        AIConfig    `json:"ai"`
	SetupDone bool        `json:"setupDone"`
	AutoStart bool        `json:"autoStart"`
}

type EmailConfig struct {
	Address  string `json:"address"`
	Username string `json:"username"`
	Password string `json:"secret"`
	Mailbox  string `json:"mailbox"`
	Enabled  bool   `json:"enabled"`
}

type AIConfig struct {
	URL     string `json:"url"`
	Key     string `json:"key"`
	Model   string `json:"model"`
	Enabled bool   `json:"enabled"`
}

type Entry struct {
	ID    string    `json:"id"`
	Title string    `json:"title"`
	State string    `json:"state"`
	Time  time.Time `json:"time"`
}

type queueItem struct {
	Entry
	Data []byte `json:"data,omitempty"`
}

type queue struct {
	Cursors map[string]string `json:"cursors"`
	Items   []queueItem       `json:"items"`
}

type Service struct {
	dir       string
	mu        sync.Mutex
	check     sync.Mutex
	config    Config
	lastError string
	queue     queue
}

func Open() (*Service, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(root, "Crier")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	s := &Service{dir: dir, config: Config{Email: EmailConfig{Mailbox: "INBOX"}, AI: AIConfig{URL: "https://api.deepseek.com/chat/completions", Model: "deepseek-chat"}}, queue: queue{Cursors: make(map[string]string)}}
	if err := readJSON(filepath.Join(dir, "settings.json"), &s.config); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read settings: %w", err)
	}
	for _, secret := range []*string{&s.config.AI.Key, &s.config.Email.Password} {
		if err := decrypt(secret); err != nil {
			return nil, fmt.Errorf("read credentials: %w", err)
		}
	}
	if err := readJSON(filepath.Join(dir, "queue.json"), &s.queue); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read print history: %w", err)
	}
	if s.queue.Cursors == nil {
		s.queue.Cursors = make(map[string]string)
	}
	return s, nil
}

func (s *Service) Config() Config {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.config
}

func (s *Service) Save(c Config) error {
	s.check.Lock()
	defer s.check.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	c.Printer = strings.TrimSpace(c.Printer)
	if c.SetupDone && c.Printer == "" {
		return errors.New("请选择打印机")
	}
	if c.Email.Enabled && (c.Email.Address == "" || c.Email.Username == "" || c.Email.Password == "") {
		return errors.New("请填写 IMAP 地址、账号和密码")
	}
	if c.AI.Enabled {
		if err := validateAI(c.AI); err != nil {
			return err
		}
	}
	disk := c
	for _, secret := range []*string{&disk.AI.Key, &disk.Email.Password} {
		if err := encrypt(secret); err != nil {
			return err
		}
	}
	if err := setAutoStart(c.AutoStart); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(s.dir, "settings.json"), disk); err != nil {
		return errors.Join(err, setAutoStart(s.config.AutoStart))
	}
	s.config = c
	return nil
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

func writeJSON(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".crier-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return replaceFile(file.Name(), path)
}
