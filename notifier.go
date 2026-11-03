// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/nicholas-fedor/shoutrrr"
	"github.com/nicholas-fedor/shoutrrr/pkg/router"
	"github.com/nicholas-fedor/shoutrrr/pkg/types"
)

// eventKind is the type of event a notification is sent for.
type eventKind string

const (
	// eventUnseal is sent when a sealed node was successfully unsealed.
	eventUnseal eventKind = "unseal"
	// eventSealStatusError is sent when checking the seal status of a node
	// fails (e.g. network errors, timeouts).
	eventSealStatusError eventKind = "seal_status_error"
	// eventUnsealError is sent when providing an unseal token to a node fails.
	eventUnsealError eventKind = "unseal_error"

	// eventGroupError is a configuration alias for all error event kinds.
	eventGroupError = "error"
)

var allEventKinds = []eventKind{eventUnseal, eventSealStatusError, eventUnsealError}

func (k eventKind) isError() bool {
	return k != eventUnseal
}

func joinEventKinds(kinds []eventKind) string {
	names := make([]string, len(kinds))
	for i, kind := range kinds {
		names[i] = string(kind)
	}
	return strings.Join(names, ", ")
}

var (
	notifyCh    = make(chan notifyEvent, 20)
	notifyQueue = []notifyEvent{}
)

type notifyEvent struct {
	timestamp time.Time
	kind      eventKind
	err       error
}

// notify logs the event, and queues it to be sent as a notification, if
// notifications are enabled for the event kind.
func notify(ctx context.Context, logger *slog.Logger, kind eventKind, err error) {
	if kind.isError() {
		err = fmt.Errorf("error: %w", err)
		logger.ErrorContext(ctx, "notification", "event", kind, "error", err)
	} else {
		err = fmt.Errorf("%s: %w", kind, err)
		logger.InfoContext(ctx, "notification", "event", kind, "message", err)
	}

	c := conf.Load()

	if len(c.Notify.URLs) == 0 {
		return
	}

	if !c.Notify.allows(kind) {
		logger.DebugContext(ctx, "notification filtered by notify events config", "event", kind)
		return
	}

	notifyCh <- notifyEvent{timestamp: time.Now(), kind: kind, err: err}
}

func notifier(ctx context.Context, logger *slog.Logger) {
	nlog := logger.With("component", "notifier")
	nlog.InfoContext(ctx, "starting notifier")

	// Wait for events, and sort of act like a debouncer. As a new event comes in,
	// it prevents the time.After statement from returning, thus resetting it's
	// timer.
	//
	// One catch though, if a certain amount of time passes since the first
	// received event, send it anyway, rather than just resetting. This ensures
	// that if a continual stream of events comes in, time.After() will *never*
	// be called, and events will continue to build up forever.
	for {
		select {
		case <-ctx.Done():
			sendQueue(ctx, nlog)
			return
		case event := <-notifyCh:
			notifyQueue = append(notifyQueue, event)

			if time.Since(notifyQueue[0].timestamp) >= conf.Load().Notify.maxElapsed {
				sendQueue(ctx, nlog)
			}
		case <-time.After(conf.Load().Notify.queueDelay):
			sendQueue(ctx, nlog)
		}
	}
}

func sendQueue(ctx context.Context, logger *slog.Logger) {
	if len(notifyQueue) == 0 {
		return
	}

	logger.InfoContext(
		ctx, "attempting to send notifications for alerts",
		"count", len(notifyQueue),
	)

	title, body := formatNotification(conf.Load().Environment, notifyQueue)

	var err error
	var r *router.ServiceRouter
	r, err = shoutrrr.NewSender(&shoutrrrLogger{logger}, conf.Load().Notify.URLs...)
	if err != nil {
		logger.ErrorContext(ctx, "unable to create shoutrrr sender", "error", err)
		return
	}

	r.Timeout = 10 * time.Second

	var services []string
	var name string
	for _, url := range conf.Load().Notify.URLs {
		name, _, err = r.ExtractServiceName(url)
		if err != nil {
			logger.ErrorContext(ctx, "unable to extract service name from URL", "error", err)
			continue
		}
		services = append(services, name)
	}

	errs := r.Send(body, &types.Params{types.TitleKey: title})

	errs = slices.DeleteFunc(errs, func(err error) bool { return err == nil })

	if len(errs) > 0 {
		// slog's JSON handler serializes a []error with encoding/json, which
		// renders most error types as "{}", so join them into a single error.
		logger.ErrorContext(
			ctx, "unable to send notifications to one or more services",
			"error", errors.Join(errs...),
			"services", strings.Join(services, ","),
		)
		return
	}

	logger.InfoContext(
		ctx, "successfully sent notifications",
		"services", strings.Join(services, ","),
	)
	notifyQueue = nil
}

// formatNotification builds the notification title and body for the queued
// events. When only errors are queued, the output matches what vault-unseal
// has always sent.
func formatNotification(environment string, events []notifyEvent) (title, body string) {
	var unseals, errs int
	for _, event := range events {
		if event.kind.isError() {
			errs++
		} else {
			unseals++
		}
	}

	var text strings.Builder
	switch {
	case unseals == 0:
		title = fmt.Sprintf("vault-unseal: %s: %d error(s) occurred", environment, errs)
		text.WriteString("vault-unseal ran into errors when attempting to check seal status/unseal. here are the errors:\n")
	case errs == 0:
		title = fmt.Sprintf("vault-unseal: %s: %d node(s) unsealed", environment, unseals)
		text.WriteString("vault-unseal successfully unsealed one or more nodes. here are the events:\n")
	default:
		title = fmt.Sprintf(
			"vault-unseal: %s: %d node(s) unsealed, %d error(s) occurred",
			environment, unseals, errs,
		)
		text.WriteString("vault-unseal unsealed one or more nodes, and ran into errors when attempting to check seal status/unseal. here are the events:\n")
	}

	for _, event := range events {
		fmt.Fprintf(&text, "\n%s :: %v", event.timestamp.Format(time.RFC3339), event.err)
	}

	fmt.Fprintf(
		&text, "\n\nsent from vault-unseal. version: %s, compile date: %s",
		version, date,
	)

	return title, text.String()
}
