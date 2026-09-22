package notify

import (
	"context"
	"time"
)

// Dispatch sends an event's alert to wherever it is routed. Events are named
// ("set_incomplete", "order_received", "security", …); see Events. It never
// blocks the caller for long and never fails loudly: an alert is best effort.
func Dispatch(event, title, body string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for _, send := range routes(event) {
		_ = send(ctx, Message{Title: title, Body: body, Tag: tagFor(event)})
	}
}

// Events are the alerts that can be routed.
var Events = []string{"security", "set_incomplete", "set_complete", "order_shipped", "order_received", "low_stock", "price_drop", "backup", "export_done"}

func tagFor(event string) string {
	switch event {
	case "security":
		return "rotating_light"
	case "set_complete", "order_received":
		return "tada"
	case "set_incomplete", "low_stock":
		return "warning"
	}
	return "bell"
}

// routes is the senders an event goes to. Until providers are configured, every
// event goes to the NOTIFY_URL sender (ntfy or a webhook) when there is one.
var routes = func(event string) []func(context.Context, Message) error {
	if s := FromConfig(); s != nil {
		return []func(context.Context, Message) error{s.Send}
	}
	return nil
}

// eventForTag maps the monitor's alert tags to routable events.
func eventForTag(tag string) string {
	switch tag {
	case "package":
		return "low_stock"
	case "moneybag":
		return "price_drop"
	case "floppy_disk":
		return "backup"
	}
	return "security"
}

// Routed is a send function for the gateway's monitor: each alert goes to the
// channels routed for its event.
func Routed(ctx context.Context, m Message) error {
	var first error
	for _, send := range routes(eventForTag(m.Tag)) {
		if err := send(ctx, m); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// Enabled reports whether any channel is configured.
func Enabled() bool {
	chans, _ := Channels()
	return len(chans) > 0
}
