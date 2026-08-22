//go:build darwin || windows

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// runNotifier watches the control API's SSE stream (adapter/control/http
// handleSubscribe, GET /subscribe?topics=log,message — the same feed the web
// admin's notification bell reads) and raises a native OS notification for
// incoming Messenger/AFP messages and error-level log lines, mirroring what
// the web UI's bell already surfaces but as a real system notification.
// Reconnects with backoff — the stream requires the process to be up and
// (once configured) authenticated, so it naturally goes quiet while stopped
// or before the admin credential is known, and picks back up once
// client.setAuth is called (see performAction).
func runNotifier(ctx context.Context, client *controlClient) {
	const minBackoff = 3 * time.Second
	const maxBackoff = 30 * time.Second
	backoff := minBackoff
	for {
		if err := streamNotifications(ctx, client); err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}
		backoff = minBackoff
	}
}

// streamNotifications holds one SSE connection open, dispatching frames as
// they arrive, until it errs out (including ctx cancellation) or the server
// closes it.
func streamNotifications(ctx context.Context, client *controlClient) error {
	resp, err := client.subscribe(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return errNonOKSubscribe
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var event string
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			dispatchEvent(client.baseURL, event, strings.TrimPrefix(line, "data: "))
		}
	}
	return scanner.Err()
}

var errNonOKSubscribe = &subscribeError{}

type subscribeError struct{}

func (*subscribeError) Error() string { return "subscribe: non-200 response" }

// dispatchEvent decodes one SSE frame's JSON payload per adapter/control/
// http's bus event shapes (core/bus.MessageReceived / core/bus.LogRecord)
// and raises a notification for the ones worth surfacing: any Messenger/AFP
// message, and error-level (Level == 4) log lines. activateURL is what
// clicking the notification opens (only honoured on Windows — see
// notify_darwin.go/notify_windows.go).
func dispatchEvent(activateURL, event, data string) {
	switch event {
	case "message":
		var m struct {
			Kind string
			From string
			Text string
		}
		if err := json.Unmarshal([]byte(data), &m); err != nil {
			return
		}
		text := strings.TrimSpace(m.Text)
		if text == "" {
			return
		}
		from := strings.TrimSpace(m.From)
		if from == "" {
			from = "Server"
		}
		if m.Kind == "messenger" {
			showNotification("Message from "+from, text, activateURL)
		} else {
			showNotification(from, text, activateURL)
		}

	case "log":
		const levelError = 4
		var l struct {
			Component string
			Level     uint8
			Msg       string
		}
		if err := json.Unmarshal([]byte(data), &l); err != nil || l.Level != levelError {
			return
		}
		showNotification(l.Component+" error", l.Msg, activateURL)
	}
}
