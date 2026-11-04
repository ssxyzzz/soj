package repo

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/event"
)

// 慢命令观测阈值行为：低于阈值静默、达到阈值打 Warn（v0.66 批①单测锁口径）。
func TestSlowOpMonitorThreshold(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(old)

	m := slowOpMonitor()
	m.Succeeded(context.Background(), &event.CommandSucceededEvent{
		CommandFinishedEvent: event.CommandFinishedEvent{
			Duration: slowOpWarnThreshold - time.Millisecond,
			CommandName: "find", DatabaseName: "soj",
		},
	})
	if strings.Contains(buf.String(), "慢 Mongo 命令") {
		t.Fatalf("低于阈值不应告警：%s", buf.String())
	}

	m.Succeeded(context.Background(), &event.CommandSucceededEvent{
		CommandFinishedEvent: event.CommandFinishedEvent{
			Duration: slowOpWarnThreshold + time.Millisecond,
			CommandName: "aggregate", DatabaseName: "soj",
		},
	})
	out := buf.String()
	if !strings.Contains(out, "慢 Mongo 命令") || !strings.Contains(out, "aggregate") {
		t.Fatalf("达到阈值应告警并带命令名：%s", out)
	}
}
