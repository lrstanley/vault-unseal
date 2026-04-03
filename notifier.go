// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/nicholas-fedor/shoutrrr"
	"github.com/nicholas-fedor/shoutrrr/pkg/router"
	"github.com/nicholas-fedor/shoutrrr/pkg/types"
)

var (
	notifyCh    = make(chan error, 20)
	notifyQueue = []notifyError{}
)

type notifyError struct {
	timestamp time.Time
	err       error
}

func notify(ctx context.Context, logger *slog.Logger, err error) {
	err = fmt.Errorf("error: %w", err)
	logger.ErrorContext(ctx, "notification", "error", err)

	if len(conf.Load().Notify.URLs) == 0 {
		return
	}

	notifyCh <- err
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
		case nerr := <-notifyCh:
			notifyQueue = append(notifyQueue, notifyError{timestamp: time.Now(), err: nerr})

			if time.Since(notifyQueue[0].timestamp) >= conf.Load().NotifyMaxElapsed {
				sendQueue(ctx, nlog)
			}
		case <-time.After(conf.Load().NotifyQueueDelay):
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

	var text strings.Builder
	text.WriteString(`vault-unseal ran into errors when attempting to check seal status/unseal. here are the errors:
`)
	for i := range notifyQueue {
		fmt.Fprintf(&text, "\n%s :: %v", notifyQueue[i].timestamp.Format(time.RFC3339), notifyQueue[i].err)
	}

	var err error

	fmt.Fprintf(
		&text, "\n\nsent from vault-unseal. version: %s, compile date: %s",
		version, date,
	)

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

	errs := r.Send(text.String(), &types.Params{
		types.TitleKey: fmt.Sprintf(
			"vault-unseal: %s: %d error(s) occurred",
			conf.Load().Environment,
			len(notifyQueue),
		),
	})

	errs = slices.DeleteFunc(errs, func(err error) bool { return err == nil })

	if len(errs) > 0 {
		logger.ErrorContext(
			ctx, "unable to send notifications to one or more services",
			"errors", errs,
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
