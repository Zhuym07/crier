package crier

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"
)

var telegramReceiptText = map[string]string{
	"printed":      "打印驱动报告已打印",
	"submitted":    "已提交到打印队列",
	"uncertain":    "打印结果未知，请检查打印机和队列",
	"acknowledged": "已在 Crier 确认收到小票",
}

func (s *Service) notifyTelegram(ctx context.Context, p plugin, todoChannel string) error {
	channel, _ := strconv.ParseInt(todoChannel, 10, 64)
	send := func(chat int64, text string) error {
		ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		return p.callWith(ctx, "notify", "", nil, map[string]any{"chat_id": chat, "text": text})
	}
	return s.flushTelegramReceipts(ctx, p.cursorKey, channel, send)
}

func (s *Service) flushTelegramReceipts(ctx context.Context, bot string, channel int64, send func(int64, string) error) error {
	s.mu.Lock()
	var pending []queueItem
	for _, job := range s.queue.Items {
		if job.TelegramBot == bot && job.TelegramChat != 0 && (job.TodoText != "" || job.State != job.NotifiedState && telegramReceiptText[job.State] != "") {
			pending = append(pending, job)
		}
	}
	s.mu.Unlock()

	var failures []error
	for _, job := range pending {
		if ctx.Err() != nil {
			return errors.Join(append(failures, ctx.Err())...)
		}
		if job.TodoText != "" {
			if channel == 0 || send(channel, job.TodoText) == nil {
				if err := s.recordTelegramNotification(job.ID, "", true); err != nil {
					return err
				}
			} else {
				failures = append(failures, errors.New("Telegram 待办频道发送失败"))
			}
		}
		if text := telegramReceiptText[job.State]; text != "" && job.State != job.NotifiedState {
			if err := send(job.TelegramChat, text+"\n"+limitText(job.Title, 160)); err != nil {
				failures = append(failures, fmt.Errorf("Telegram 打印回执发送失败: %w", err))
				continue
			}
			if err := s.recordTelegramNotification(job.ID, job.State, false); err != nil {
				return err
			}
		}
	}
	return errors.Join(failures...)
}

func (s *Service) recordTelegramNotification(id, state string, todoSent bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.queue.Items {
		job := &s.queue.Items[i]
		if job.ID != id {
			continue
		}
		old := *job
		if todoSent {
			job.TodoText = ""
		} else {
			job.NotifiedState = state
		}
		if err := s.saveQueue(); err != nil {
			*job = old
			return err
		}
		return nil
	}
	return errors.New("找不到 Telegram 通知对应的小票")
}
