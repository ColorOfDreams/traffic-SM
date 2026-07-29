package eventstream

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

// File này chủ yếu tạo consumer, poll, commit

func newConsumer(config Config, clientID string, topic string, group string) (*kgo.Client, error) {
	client, err := kgo.NewClient(
		kgo.SeedBrokers(config.Brokers...),
		kgo.ClientID(clientID),
		kgo.ConsumeTopics(topic),
		kgo.ConsumerGroup(group),
		kgo.Balancers(kgo.CooperativeStickyBalancer()),
		kgo.AutoCommitMarks(),
		kgo.AutoCommitInterval(config.CommitInterval),
	)
	if err != nil {
		return nil, fmt.Errorf("create Kafka consumer %s: %w", clientID, err)
	}
	return client, nil
}

func (stream *Kafka) poll(
	client *kgo.Client,
	ctx context.Context,
	maxRecords int,
) kgo.Fetches {
	pollContext, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	return client.PollRecords(pollContext, maxRecords)
}

func logFetchErrors(topic string, fetches kgo.Fetches) {
	for _, fetchError := range fetches.Errors() {
		if errors.Is(fetchError.Err, context.DeadlineExceeded) ||
			errors.Is(fetchError.Err, context.Canceled) {
			continue
		}
		log.Printf(
			"poll Kafka topic=%s partition=%d: %v",
			topic,
			fetchError.Partition,
			fetchError.Err,
		)
	}
}

func (stream *Kafka) commit(parent context.Context, client *kgo.Client) {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	if err := client.CommitMarkedOffsets(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("commit Kafka offsets: %v", err)
	}
}
