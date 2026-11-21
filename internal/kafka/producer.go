package kafka

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/IBM/sarama"

	"github.com/dloshkarev/highloadarchitect/internal/config"
	"github.com/dloshkarev/highloadarchitect/internal/domain"
)

// syncProducer — часть sarama, нужная продюсеру.
type syncProducer interface {
	SendMessage(msg *sarama.ProducerMessage) (int32, int64, error)
	Close() error
}

// Producer публикует новые посты в Kafka. Работает через idempotent-продюсер
// sarama: дубли исключаются на уровне брокера, а порядок постов одного автора
// сохраняется внутри его партиции.
type Producer struct {
	producer syncProducer
	topic    string
}

func NewProducer(cfg config.KafkaConfig) (*Producer, error) {
	brokers, err := cfg.BrokersList()
	if err != nil {
		return nil, fmt.Errorf("parse kafka brokers: %w", err)
	}

	producer, err := sarama.NewSyncProducer(brokers, producerConfig())
	if err != nil {
		return nil, fmt.Errorf("create kafka producer: %w", err)
	}

	return newProducer(producer, cfg.Topic), nil
}

func newProducer(producer syncProducer, topic string) *Producer {
	return &Producer{producer: producer, topic: topic}
}

// Publish отправляет сообщение с ключом author_id: все посты одного автора
// попадают в одну партицию и сохраняют порядок.
func (p *Producer) Publish(ctx context.Context, post domain.PublishedPost) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("publish new post: %w", err)
	}

	value, err := json.Marshal(post)
	if err != nil {
		return fmt.Errorf("marshal published post: %w", err)
	}

	msg := &sarama.ProducerMessage{
		Topic: p.topic,
		Key:   sarama.StringEncoder(post.AuthorID),
		Value: sarama.ByteEncoder(value),
	}

	if _, _, err := p.producer.SendMessage(msg); err != nil {
		return fmt.Errorf("write kafka message: %w", err)
	}

	return nil
}

func (p *Producer) Close() error {
	if err := p.producer.Close(); err != nil {
		return fmt.Errorf("close kafka producer: %w", err)
	}

	return nil
}

// producerConfig включает строгий idempotent-режим продюсера: требования
// sarama к нему — WaitForAll, Return.Successes и один запрос на брокер за раз.
// Транзитные ошибки брокера ретраятся внутри продюсера.
func producerConfig() *sarama.Config {
	cfg := sarama.NewConfig()
	cfg.Producer.Idempotent = true
	cfg.Producer.RequiredAcks = sarama.WaitForAll
	cfg.Producer.Return.Successes = true
	cfg.Net.MaxOpenRequests = 1
	cfg.Producer.Retry.Max = 10

	return cfg
}
