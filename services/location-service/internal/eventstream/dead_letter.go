package eventstream

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/gps"
)

type deadLetterEvent struct {
	Stage      string     `json:"stage"`
	Error      string     `json:"error"`
	FailedAtMS int64      `json:"failed_at_ms"`
	Trace      *gps.Trace `json:"trace,omitempty"`
	RawEvent   []byte     `json:"raw_event,omitempty"`
}

func (stream *Kafka) publishDeadLetterWithRetry(
	ctx context.Context,
	key string,
	event deadLetterEvent,
) (bool, error) {
	value, err := json.Marshal(event)
	if err != nil {
		return false, fmt.Errorf("encode dead-letter event: %w", err)
	}
	if err := stream.produceWithRetry(ctx, stream.deadLetterTopic, key, value); err != nil {
		return false, err
	}
	return true, nil
}
