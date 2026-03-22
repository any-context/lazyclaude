package gui

import (
	"testing"
	"time"

	"github.com/KEMSHlM/lazyclaude/internal/core/event"
	"github.com/KEMSHlM/lazyclaude/internal/core/model"
)

func TestNotifyLoop_NotifyOutput(t *testing.T) {
	nl := NewNotifyLoop()
	nl.NotifyOutput()
	// Should not block, channel is buffered
	select {
	case <-nl.OutputCh():
	default:
		t.Error("expected signal on OutputCh")
	}
}

func TestNotifyLoop_NotifyOutput_NonBlocking(t *testing.T) {
	nl := NewNotifyLoop()
	nl.NotifyOutput()
	nl.NotifyOutput() // second call should not block (already signaled)
}

func TestNotifyLoop_SetOnTick(t *testing.T) {
	nl := NewNotifyLoop()
	called := false
	nl.SetOnTick(func() { called = true })
	nl.OnTick()
	if !called {
		t.Error("onTick not called")
	}
}

func TestNotifyLoop_SetBroker(t *testing.T) {
	nl := NewNotifyLoop()
	broker := event.NewBroker[model.Event]()
	defer broker.Close()
	nl.SetBroker(broker)
	if nl.BrokerCh() == nil {
		t.Error("brokerCh should not be nil after SetBroker")
	}
}

func TestNotifyLoop_SetBroker_Nil(t *testing.T) {
	nl := NewNotifyLoop()
	nl.SetBroker(nil) // should not panic
	if nl.BrokerCh() != nil {
		t.Error("brokerCh should be nil when broker is nil")
	}
}

func TestNotifyLoop_BrokerEvent(t *testing.T) {
	nl := NewNotifyLoop()
	broker := event.NewBroker[model.Event]()
	defer broker.Close()
	nl.SetBroker(broker)

	n := &model.ToolNotification{ToolName: "Write", Window: "@1"}
	broker.Publish(model.Event{Notification: n})

	// Give broker time to deliver
	time.Sleep(50 * time.Millisecond)

	select {
	case ev := <-nl.BrokerCh():
		if ev.Notification == nil || ev.Notification.ToolName != "Write" {
			t.Errorf("unexpected event: %+v", ev)
		}
	default:
		t.Error("no event on brokerCh")
	}
}

func TestNotifyLoop_Cancel(t *testing.T) {
	nl := NewNotifyLoop()
	broker := event.NewBroker[model.Event]()
	defer broker.Close()
	nl.SetBroker(broker)
	nl.Cancel()
	// After cancel, brokerCh should still exist but subscription cancelled
}
