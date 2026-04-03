// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	mail "gopkg.in/mail.v2"
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
	logger.ErrorContext(ctx, "notification", "err", err)

	if !conf.Load().Email.Enabled {
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
		fmt.Fprintf(&text, "\n%s :: %v", notifyQueue[i].timestamp.Format(time.RFC822), notifyQueue[i].err)
	}

	var err error

	smtp := mail.NewDialer(
		conf.Load().Email.Hostname,
		conf.Load().Email.Port,
		conf.Load().Email.Username,
		conf.Load().Email.Password,
	)
	smtp.TLSConfig = &tls.Config{
		InsecureSkipVerify: conf.Load().Email.TLSSkipVerify, //nolint:gosec
		ServerName:         conf.Load().Email.Hostname,
	}

	if conf.Load().Email.MandatoryTLS {
		smtp.StartTLSPolicy = mail.MandatoryStartTLS
	}

	smtp.LocalName, err = os.Hostname()
	if err != nil {
		smtp.LocalName = "localhost"
	}

	s, err := smtp.Dial()
	if err != nil {
		logger.ErrorContext(
			ctx, "unable to make smtp connection",
			"error", err,
			"hostname", conf.Load().Email.Hostname,
			"port", conf.Load().Email.Port,
		)
		return
	}

	fmt.Fprintf(
		&text, "\n\nsent from vault-unseal. version: %s, compile date: %s, hostname: %s",
		version, date, smtp.LocalName,
	)

	msg := mail.NewMessage()
	msg.SetHeader("From", conf.Load().Email.FromAddr)
	msg.SetHeader(
		"Subject",
		fmt.Sprintf(
			"vault-unseal: %s: %d errors occurred",
			conf.Load().Environment,
			len(notifyQueue),
		),
	)
	msg.SetBody("text/plain", text.String())

	msg.SetHeader("To", conf.Load().Email.SendAddrs[0])
	if len(conf.Load().Email.SendAddrs) > 1 {
		msg.SetHeader("CC", conf.Load().Email.SendAddrs[1:]...)
	}

	err = mail.Send(s, msg)
	if err != nil {
		logger.ErrorContext(ctx, "unable to send notification", "error", err)
		return
	}

	logger.InfoContext(
		ctx, "successfully sent notifications",
		"to", strings.Join(conf.Load().Email.SendAddrs, ","),
	)
	notifyQueue = nil
}
