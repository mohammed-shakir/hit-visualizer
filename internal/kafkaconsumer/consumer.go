// Package kafkaconsumer implements a Kafka consumer for hit events.
package kafkaconsumer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/IBM/sarama"
	h3 "github.com/uber/h3-go/v4"
)

type HitEvent struct {
	Layer     string  `json:"layer"`
	Lon       float64 `json:"lon"`
	Lat       float64 `json:"lat"`
	Timestamp string  `json:"timestamp"`
	Res       int     `json:"res,omitempty"`
	Scenario  string  `json:"scenario,omitempty"`
	Cell      string  `json:"cell,omitempty"`
}

type Consumer struct {
	group  sarama.ConsumerGroup
	topic  string
	events chan HitEvent
	logger *slog.Logger
}

func NewConsumer(brokers []string, groupID, topic string, logger *slog.Logger) (*Consumer, error) {
	if logger == nil {
		logger = slog.Default()
	}

	cfg := sarama.NewConfig()
	cfg.Version = sarama.V2_5_0_0
	cfg.Consumer.Offsets.Initial = sarama.OffsetNewest
	cfg.Consumer.Return.Errors = true

	group, err := sarama.NewConsumerGroup(brokers, groupID, cfg)
	if err != nil {
		return nil, fmt.Errorf("kafka consumer group: %w", err)
	}

	c := &Consumer{
		group:  group,
		topic:  topic,
		events: make(chan HitEvent, 1024),
		logger: logger,
	}

	go func() {
		for err := range group.Errors() {
			if err != nil {
				c.logger.Error("kafka consumer group error", "err", err)
			}
		}
	}()

	return c, nil
}

func (c *Consumer) Events() <-chan HitEvent {
	return c.events
}

func (c *Consumer) Start(ctx context.Context) error {
	defer close(c.events)

	handler := &consumerGroupHandler{consumer: c}

	for {
		if err := c.group.Consume(ctx, []string{c.topic}, handler); err != nil {
			c.logger.Error("kafka consume", "err", err)
			select {
			case <-time.After(time.Second):
			case <-ctx.Done():
				return nil
			}
		}

		if ctx.Err() != nil {
			return nil
		}
	}
}

func (c *Consumer) Close() error {
	if c.group == nil {
		return nil
	}
	if err := c.group.Close(); err != nil {
		return fmt.Errorf("kafka consumer group close: %w", err)
	}
	return nil
}

type consumerGroupHandler struct {
	consumer *Consumer
}

func (h *consumerGroupHandler) Setup(sarama.ConsumerGroupSession) error {
	h.consumer.logger.Info("kafka consumer group session setup")
	return nil
}

func (h *consumerGroupHandler) Cleanup(sarama.ConsumerGroupSession) error {
	h.consumer.logger.Info("kafka consumer group session cleanup")
	return nil
}

func (h *consumerGroupHandler) ConsumeClaim(
	sess sarama.ConsumerGroupSession,
	claim sarama.ConsumerGroupClaim,
) error {
	ctx := sess.Context()

	for msg := range claim.Messages() {
		var evt HitEvent
		if err := json.Unmarshal(msg.Value, &evt); err != nil {
			h.consumer.logger.Error("decode hit event", "err", err)
			sess.MarkMessage(msg, "")
			continue
		}

		if evt.Lon == 0 && evt.Lat == 0 && evt.Cell != "" {
			idx := h3.IndexFromString(evt.Cell)
			cell := h3.Cell(idx)
			if cell.IsValid() {
				ll, err := h3.CellToLatLng(cell)
				if err != nil {
					h.consumer.logger.Warn("convert h3 cell to lat/lng", "cell", evt.Cell, "err", err)
				} else {
					evt.Lat = ll.Lat
					evt.Lon = ll.Lng
				}
			} else {
				h.consumer.logger.Warn("invalid h3 cell in hit event", "cell", evt.Cell)
			}
		}

		if evt.Lon == 0 && evt.Lat == 0 {
			h.consumer.logger.Warn("hit event missing coordinates, skipping",
				"layer", evt.Layer,
				"timestamp", evt.Timestamp,
			)
			sess.MarkMessage(msg, "")
			continue
		}

		select {
		case <-ctx.Done():
			return nil
		case h.consumer.events <- evt:
			// forwarded
		default:
			h.consumer.logger.Warn("hit event channel full, dropping",
				"layer", evt.Layer,
				"timestamp", evt.Timestamp,
			)
		}

		sess.MarkMessage(msg, "")
	}

	return nil
}
