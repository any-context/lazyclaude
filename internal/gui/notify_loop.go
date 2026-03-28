package gui

import (
	"github.com/KEMSHlM/lazyclaude/internal/core/event"
	"github.com/KEMSHlM/lazyclaude/internal/core/model"
)

// NotifyLoop manages notification delivery state: output signals,
// event broker subscription, and tick callbacks.
type NotifyLoop struct {
	outputNotify chan struct{}
	broker       *event.Broker[model.Event]
	brokerSub    *event.Subscription[model.Event]
	onTick       func()
}

// NewNotifyLoop creates a NotifyLoop.
func NewNotifyLoop() *NotifyLoop {
	return &NotifyLoop{
		outputNotify: make(chan struct{}, 1),
	}
}

// OutputCh returns the channel signaled when pane output arrives.
func (nl *NotifyLoop) OutputCh() <-chan struct{} {
	return nl.outputNotify
}

// NotifyOutput signals that a pane has new output. Non-blocking.
func (nl *NotifyLoop) NotifyOutput() {
	select {
	case nl.outputNotify <- struct{}{}:
	default:
	}
}

// SetBroker attaches an event broker for immediate notification delivery.
// Passing nil is a no-op.
func (nl *NotifyLoop) SetBroker(broker *event.Broker[model.Event]) {
	if broker == nil {
		return
	}
	nl.broker = broker
	nl.brokerSub = broker.Subscribe(8)
}

// BrokerCh returns the broker event channel, or nil if no broker is set.
func (nl *NotifyLoop) BrokerCh() <-chan model.Event {
	if nl.brokerSub == nil {
		return nil
	}
	return nl.brokerSub.Ch()
}

// HasBroker returns true if an event broker is wired for direct delivery.
// When true, file-based polling is redundant and should be skipped.
func (nl *NotifyLoop) HasBroker() bool {
	return nl.brokerSub != nil
}

// SetOnTick sets a callback invoked every ticker cycle.
func (nl *NotifyLoop) SetOnTick(fn func()) {
	nl.onTick = fn
}

// OnTick invokes the tick callback if set.
func (nl *NotifyLoop) OnTick() {
	if nl.onTick != nil {
		nl.onTick()
	}
}

// Cancel cancels the broker subscription if active.
func (nl *NotifyLoop) Cancel() {
	if nl.brokerSub != nil {
		nl.brokerSub.Cancel()
	}
}
