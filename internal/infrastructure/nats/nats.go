package nats

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	natsgo "github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

// NATSClient is a wrapper around the NATS client.
type NATSClient struct {
	client *natsgo.Conn
	js     natsgo.JetStreamContext
}

// Config holds NATS configuration.
type Config struct {
	URL string

	Name string

	ConnectTimeout time.Duration
	ReconnectWait  time.Duration
	MaxReconnects  int
}

// NewClient creates and initializes a new NATS client.
func NewClient(
	config Config,
	log *zap.Logger,
) (*NATSClient, error) {

	connectTimeout := config.ConnectTimeout
	if connectTimeout == 0 {
		connectTimeout = 5 * time.Second
	}

	reconnectWait := config.ReconnectWait
	if reconnectWait == 0 {
		reconnectWait = 2 * time.Second
	}

	maxReconnects := config.MaxReconnects

	options := []natsgo.Option{
		natsgo.Timeout(connectTimeout),
		natsgo.ReconnectWait(reconnectWait),
		natsgo.MaxReconnects(maxReconnects),
	}

	if config.Name != "" {
		options = append(
			options,
			natsgo.Name(config.Name),
		)
	}

	client, err := natsgo.Connect(
		config.URL,
		options...,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}

	// Initialize JetStream.
	js, err := client.JetStream()
	if err != nil {
		client.Close()

		return nil, fmt.Errorf(
			"failed to initialize JetStream: %w",
			err,
		)
	}

	log.Info(
		"NATS connected",
		zap.String("url", config.URL),
	)

	return &NATSClient{
		client: client,
		js:     js,
	}, nil
}

// Client returns the underlying NATS connection.
func (n *NATSClient) Client() *natsgo.Conn {
	return n.client
}

// JetStream returns the JetStream context.
func (n *NATSClient) JetStream() natsgo.JetStreamContext {
	return n.js
}

// Close closes the NATS connection.
func (n *NATSClient) Close() {
	if n.client == nil {
		return
	}

	n.client.Drain()
	n.client.Close()
}

// Ping checks whether NATS is available.
func (n *NATSClient) Ping() error {
	if n.client == nil {
		return fmt.Errorf("NATS client is nil")
	}

	if !n.client.IsConnected() {
		return fmt.Errorf("NATS is not connected")
	}

	return nil
}

// ----------------------------------------------------------------------------
// PUBLISH
// ----------------------------------------------------------------------------

// Publish publishes raw bytes to a subject.
func (n *NATSClient) Publish(
	ctx context.Context,
	subject string,
	data []byte,
) error {

	if n.client == nil {
		return fmt.Errorf("NATS client is nil")
	}

	msg := &natsgo.Msg{
		Subject: subject,
		Data:    data,
	}

	return n.client.PublishMsg(msg)
}

// PublishJSON publishes a JSON payload to a subject.
func (n *NATSClient) PublishJSON(
	ctx context.Context,
	subject string,
	data interface{},
) error {

	payload, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf(
			"failed to marshal NATS payload: %w",
			err,
		)
	}

	return n.Publish(
		ctx,
		subject,
		payload,
	)
}

// ----------------------------------------------------------------------------
// REQUEST / RESPONSE
// ----------------------------------------------------------------------------

// Request sends a request and waits for a response.
func (n *NATSClient) Request(
	ctx context.Context,
	subject string,
	data []byte,
	timeout time.Duration,
) (*natsgo.Msg, error) {

	if n.client == nil {
		return nil, fmt.Errorf("NATS client is nil")
	}

	msg := &natsgo.Msg{
		Subject: subject,
		Data:    data,
	}

	return n.client.RequestMsg(
		msg,
		timeout,
	)
}

// RequestJSON sends a JSON request and waits for a response.
func (n *NATSClient) RequestJSON(
	ctx context.Context,
	subject string,
	data interface{},
	timeout time.Duration,
) (*natsgo.Msg, error) {

	payload, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to marshal request payload: %w",
			err,
		)
	}

	return n.Request(
		ctx,
		subject,
		payload,
		timeout,
	)
}

// ----------------------------------------------------------------------------
// SUBSCRIBE
// ----------------------------------------------------------------------------

// Subscribe subscribes to a NATS subject.
func (n *NATSClient) Subscribe(
	subject string,
	handler natsgo.MsgHandler,
) (*natsgo.Subscription, error) {

	if n.client == nil {
		return nil, fmt.Errorf("NATS client is nil")
	}

	return n.client.Subscribe(
		subject,
		handler,
	)
}

// QueueSubscribe creates a queue subscription.
func (n *NATSClient) QueueSubscribe(
	subject string,
	queue string,
	handler natsgo.MsgHandler,
) (*natsgo.Subscription, error) {

	if n.client == nil {
		return nil, fmt.Errorf("NATS client is nil")
	}

	return n.client.QueueSubscribe(
		subject,
		queue,
		handler,
	)
}

// ----------------------------------------------------------------------------
// JETSTREAM
// ----------------------------------------------------------------------------

// PublishPersistent publishes a message to JetStream.
func (n *NATSClient) PublishPersistent(
	ctx context.Context,
	subject string,
	data []byte,
) (*natsgo.PubAck, error) {

	if n.js == nil {
		return nil, fmt.Errorf("JetStream is not initialized")
	}

	return n.js.Publish(
		subject,
		data,
	)
}

// PublishPersistentJSON publishes JSON to JetStream.
func (n *NATSClient) PublishPersistentJSON(
	ctx context.Context,
	subject string,
	data interface{},
) (*natsgo.PubAck, error) {

	payload, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to marshal JetStream payload: %w",
			err,
		)
	}

	return n.PublishPersistent(
		ctx,
		subject,
		payload,
	)
}

// ----------------------------------------------------------------------------
// JETSTREAM STREAMS
// ----------------------------------------------------------------------------

// AddStream creates or updates a JetStream stream.
func (n *NATSClient) AddStream(
	config *natsgo.StreamConfig,
) (*natsgo.StreamInfo, error) {

	if n.js == nil {
		return nil, fmt.Errorf("JetStream is not initialized")
	}

	return n.js.AddStream(config)
}

// GetStream returns information about a stream.
func (n *NATSClient) GetStream(
	name string,
) (*natsgo.StreamInfo, error) {

	if n.js == nil {
		return nil, fmt.Errorf("JetStream is not initialized")
	}

	return n.js.StreamInfo(name)
}

// DeleteStream deletes a JetStream stream.
func (n *NATSClient) DeleteStream(
	name string,
) error {

	if n.js == nil {
		return fmt.Errorf("JetStream is not initialized")
	}

	return n.js.DeleteStream(name)
}

// ----------------------------------------------------------------------------
// JETSTREAM CONSUMERS
// ----------------------------------------------------------------------------

// AddConsumer creates or updates a JetStream consumer.
func (n *NATSClient) AddConsumer(
	stream string,
	config *natsgo.ConsumerConfig,
) (*natsgo.ConsumerInfo, error) {

	if n.js == nil {
		return nil, fmt.Errorf("JetStream is not initialized")
	}

	return n.js.AddConsumer(
		stream,
		config,
	)
}
