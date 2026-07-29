package eventstream

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ColorOfDreams/traffic-system/services/location-service/internal/gps"
	"github.com/twmb/franz-go/pkg/kgo"
)

type Config struct {
	Brokers           []string
	RawTopic          string
	TraceTopic        string
	MatchedTopic      string
	DeadLetterTopic   string
	TraceBuilderGroup string
	MatcherGroup      string
	Workers           int
	JobQueueSize      int
	MaxMatchAttempts  int
	CommitInterval    time.Duration
	State             gps.ConsumerStateConfig
}

type Kafka struct {
	producer        *kgo.Client
	rawConsumer     *kgo.Client
	traceConsumer   *kgo.Client
	rawTopic        string
	traceTopic      string
	matchedTopic    string
	deadLetterTopic string
	config          Config
}

func New(config Config) (*Kafka, error) {
	if err := validateConfig(config); err != nil {
		return nil, err
	}

	producer, err := kgo.NewClient(
		kgo.SeedBrokers(config.Brokers...),
		kgo.ClientID("traffic-location-producer"),
		kgo.ProducerBatchMaxBytes(1*1024*1024),
		kgo.ProducerLinger(5*time.Millisecond),
		kgo.RequiredAcks(kgo.AllISRAcks()),
	)
	if err != nil {
		return nil, fmt.Errorf("create Kafka producer: %w", err)
	}

	rawConsumer, err := newConsumer(
		config,
		"traffic-trace-builder",
		config.RawTopic,
		config.TraceBuilderGroup,
	)
	if err != nil {
		producer.Close()
		return nil, err
	}

	traceConsumer, err := newConsumer(
		config,
		"traffic-map-matcher",
		config.TraceTopic,
		config.MatcherGroup,
	)
	if err != nil {
		rawConsumer.Close()
		producer.Close()
		return nil, err
	}

	return &Kafka{
		producer:        producer,
		rawConsumer:     rawConsumer,
		traceConsumer:   traceConsumer,
		rawTopic:        config.RawTopic,
		traceTopic:      config.TraceTopic,
		matchedTopic:    config.MatchedTopic,
		deadLetterTopic: config.DeadLetterTopic,
		config:          config,
	}, nil
}

func validateConfig(config Config) error {
	if len(config.Brokers) == 0 {
		return errors.New("at least one Kafka broker is required")
	}
	if config.RawTopic == "" || config.TraceTopic == "" ||
		config.MatchedTopic == "" || config.DeadLetterTopic == "" {
		return errors.New("Kafka topic names are required")
	}
	if config.TraceBuilderGroup == "" || config.MatcherGroup == "" {
		return errors.New("Kafka consumer group names are required")
	}
	if config.TraceBuilderGroup == config.MatcherGroup {
		return errors.New("trace builder and map matcher must use different consumer groups")
	}
	if config.Workers < 1 {
		return errors.New("GraphHopper worker count must be at least one")
	}
	if config.JobQueueSize < config.Workers {
		return errors.New("match job queue must be at least worker count")
	}
	if config.MaxMatchAttempts < 1 {
		return errors.New("maximum match attempts must be at least one")
	}
	if config.CommitInterval <= 0 {
		return errors.New("Kafka commit interval must be positive")
	}
	return nil
}

func (stream *Kafka) Ping(ctx context.Context) error {
	if err := stream.producer.Ping(ctx); err != nil {
		return fmt.Errorf("ping Kafka: %w", err)
	}
	return nil
}

func (stream *Kafka) Close() {
	stream.traceConsumer.Close()
	stream.rawConsumer.Close()
	stream.producer.Close()
}

func (stream *Kafka) Run(ctx context.Context, matcher Matcher) error {
	runContext, cancel := context.WithCancel(ctx)
	defer cancel()

	errorsChannel := make(chan error, 2)
	go func() {
		errorsChannel <- stream.runTraceBuilder(runContext)
	}()
	go func() {
		errorsChannel <- stream.runMapMatcher(runContext, matcher)
	}()

	var runErr error
	for completed := 0; completed < 2; completed++ {
		err := <-errorsChannel
		if err != nil && runErr == nil {
			runErr = err
			cancel()
		}
	}
	return runErr
}
