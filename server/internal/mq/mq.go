// Package mq 封装 RabbitMQ 的生产者（Web 服务投递判题任务）
// 与消费者（判题 worker）。
package mq

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

type submissionMessage struct {
	SubmissionID int64 `json:"submission_id"`
}

// 容器编排下 RabbitMQ 可能比本服务晚就绪，带重试地拨号。
func dialWithRetry(url string) (*amqp.Connection, error) {
	var lastErr error
	for i := 0; i < 30; i++ {
		conn, err := amqp.Dial(url)
		if err == nil {
			return conn, nil
		}
		lastErr = err
		log.Printf("等待 RabbitMQ 就绪(%s): %v", url, err)
		time.Sleep(2 * time.Second)
	}
	return nil, fmt.Errorf("连接 RabbitMQ 失败: %w", lastErr)
}

func declareQueue(ch *amqp.Channel, queue string) error {
	_, err := ch.QueueDeclare(queue, true, false, false, false, nil)
	return err
}

// Publisher 向判题队列投递提交 ID。
type Publisher struct {
	ch    *amqp.Channel
	queue string
}

func NewPublisher(url, queue string) (*Publisher, error) {
	conn, err := dialWithRetry(url)
	if err != nil {
		return nil, err
	}
	ch, err := conn.Channel()
	if err != nil {
		return nil, err
	}
	if err := declareQueue(ch, queue); err != nil {
		return nil, err
	}
	return &Publisher{ch: ch, queue: queue}, nil
}

func (p *Publisher) PublishSubmission(ctx context.Context, submissionID int64) error {
	body, err := json.Marshal(submissionMessage{SubmissionID: submissionID})
	if err != nil {
		return err
	}
	return p.ch.PublishWithContext(ctx, "", p.queue, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Timestamp:    time.Now(),
		Body:         body,
	})
}

// Consume 阻塞消费判题队列：workers 个协程并发判题，ctx 取消后优雅退出。
// 骨架版在任务分发后立即 Ack（at-most-once）：判题中途崩溃会丢失该条任务；
// 改为判题完成后再 Ack 即可升级为 at-least-once（需配合状态防重判）。
func Consume(ctx context.Context, url, queue string, workers int, handle func(context.Context, int64)) error {
	if workers <= 0 {
		workers = 1
	}
	conn, err := dialWithRetry(url)
	if err != nil {
		return err
	}
	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	if err := declareQueue(ch, queue); err != nil {
		return err
	}
	if err := ch.Qos(workers*2, 0, false); err != nil {
		return err
	}
	msgs, err := ch.Consume(queue, "", false, false, false, false, nil)
	if err != nil {
		return err
	}

	jobs := make(chan int64)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range jobs {
				handle(ctx, id)
			}
		}()
	}

	log.Printf("[mq] 判题消费者已启动: queue=%s workers=%d", queue, workers)
	for {
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return nil
		case d, ok := <-msgs:
			if !ok {
				close(jobs)
				wg.Wait()
				return fmt.Errorf("RabbitMQ 通道意外关闭")
			}
			var m submissionMessage
			if err := json.Unmarshal(d.Body, &m); err != nil {
				log.Printf("[mq] 丢弃无法解析的消息: %v", err)
				_ = d.Ack(false)
				continue
			}
			jobs <- m.SubmissionID
			_ = d.Ack(false)
		}
	}
}
