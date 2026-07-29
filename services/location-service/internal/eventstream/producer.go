package eventstream

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/gps"
	"github.com/twmb/franz-go/pkg/kgo"
)

// PublishGPS ghi một GPS event đã được validate vào raw topic.
func (stream *Kafka) PublishGPS(ctx context.Context, event gps.CanonicalEvent) error {
	value, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode GPS event: %w", err)
	}
	if err := stream.produceSync(ctx, stream.rawTopic, event.DriverID, value); err != nil {
		return fmt.Errorf("produce GPS event: %w", err)
	}
	return nil
}

func (stream *Kafka) produceWithRetry(
	ctx context.Context,
	topic string,
	key string,
	value []byte,
) error {
	for {
		err := stream.produceSync(ctx, topic, key, value)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		log.Printf("produce topic=%s key=%s failed; retrying: %v", topic, key, err)
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (stream *Kafka) produceSync(
	ctx context.Context,
	topic string,
	key string,
	value []byte,
) error {
	results := stream.producer.ProduceSync(ctx, &kgo.Record{
		Topic: topic,
		Key:   []byte(key),
		Value: value,
	})
	if err := results.FirstErr(); err != nil {
		return fmt.Errorf("produce topic=%s: %w", topic, err)
	}
	return nil
}
