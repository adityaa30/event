package event

import (
	"context"

	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type natsImpl struct {
	nc      *nats.Conn
	sub     *nats.Subscription
	onError func(Event, error)
	localImpl
}

// Nats events
func Nats(name string, nc *nats.Conn, onError func(Event, error)) Event {
	return &natsImpl{
		nc:      nc,
		onError: onError,
		localImpl: localImpl{
			name: name,
			published: promauto.NewCounter(prometheus.CounterOpts{
				Namespace: "event",
				Subsystem: strip(name),
				Name:      "published",
				Help:      "Total messages published",
			}),
			handled: promauto.NewCounter(prometheus.CounterOpts{
				Namespace: "event",
				Subsystem: strip(name),
				Name:      "handled",
				Help:      "Total messages handled by subscribers",
			})},
	}
}

// Publish ...
func (e *natsImpl) Publish(ctx context.Context, data Data) {
	msg := &RemoteMsg{Data: data, Source: defaultSource}
	// Set event id if not already set
	if msg.ID = ContextEventID(ctx); msg.ID == "" {
		msg.ID = NewID()
		ctx = ContextWithEventID(ctx, msg.ID)
	}
	// Set sender id
	if msg.Source = ContextSource(ctx); msg.Source == "" {
		msg.Source = defaultSource
		ctx = ContextWithSource(ctx, msg.Source)
	}
	e.localImpl.Publish(ctx, data)
	d, err := Marshal(msg)
	if err == nil {
		if err := e.nc.Publish(e.name, d); err != nil {
			logger.Printf("Publish msg error: %v", err)
			if e.onError != nil {
				e.onError(e, err)
			}
		}
	} else {
		logger.Printf("encode msg error: %v", err)
		if e.onError != nil {
			e.onError(e, err)
		}
	}
}

// Subscribe ...
func (e *natsImpl) Subscribe(ctx context.Context, handler Handler) {
	var err error
	e.localImpl.Subscribe(ctx, handler)
	e.mutex.Lock()
	defer e.mutex.Unlock()
	if e.sub != nil {
		return
	}
	if e.nc == nil {
		logger.Printf("Error!!!, nats connection null")
		return
	}
	e.sub, err = e.nc.Subscribe(e.name, func(msg *nats.Msg) {
		data, err := Unmarshal(msg.Data)
		if err != nil {
			logger.Printf("decode msg error: %v", err)
			if e.onError != nil {
				e.onError(e, err)
			}
		} else if e.onError != nil {
			e.onError(e, err)
		}
		// Publish with new context
		e.localImpl.Publish(
			ContextWithMetadata(
				ContextWithSource(
					ContextWithEventID(ctx, data.ID),
					data.Source),
				data.Metadata),
			data.Data)
	})
	if err != nil {
		logger.Printf("Error on nc subscribe: %v", err)
	}
}
