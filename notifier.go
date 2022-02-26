// Copyright (c) Liam Stanley <me@liamstanley.io>. All rights reserved. Use
// of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"crypto/tls"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/apex/log"
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

func notify(err error) {
	err = errors.New("error: " + err.Error())
	logger.WithError(err).Error("notify-error")

	if !conf.Email.Enabled {
		return
	}

	notifyCh <- err
}

func notifier(done chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()

	logger.Info("starting notifier")

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
		case <-done:
			sendQueue()
			return
		case err := <-notifyCh:
			notifyQueue = append(notifyQueue, notifyError{timestamp: time.Now(), err: err})

			if time.Since(notifyQueue[0].timestamp) >= conf.NotifyMaxElapsed {
				sendQueue()
			}
		case <-time.After(conf.NotifyQueueDelay):
			sendQueue()
		}
	}
}

func sendQueue() {
	if len(notifyQueue) == 0 {
		return
	}

	logger.Infof("attempting to send notifications for %d alerts", len(notifyQueue))

	text := `vault-unseal ran into errors when attempting to check seal status/unseal. here are the errors:
`

	for i := 0; i < len(notifyQueue); i++ {
		text += fmt.Sprintf("\n%s :: %v", notifyQueue[i].timestamp.Format(time.RFC822), notifyQueue[i].err)
	}

	var err error

	smtp := mail.NewDialer(conf.Email.Hostname, conf.Email.Port, conf.Email.Username, conf.Email.Password)
	smtp.TLSConfig = &tls.Config{InsecureSkipVerify: conf.Email.TLSSkipVerify}
	if conf.Email.MandatoryTLS {
		smtp.StartTLSPolicy = mail.MandatoryStartTLS
	}
	smtp.LocalName, err = os.Hostname()
	if err != nil {
		smtp.LocalName = "localhost"
	}
	s, err := smtp.Dial()
	if err != nil {
		logger.WithError(err).WithFields(log.Fields{
			"hostname": conf.Email.Hostname,
			"port":     conf.Email.Port,
		}).Error("unable to make smtp connection")
		return
	}

	text += fmt.Sprintf("\n\nsent from vault-unseal. version: %s, compile date: %s, hostname: %s", version, date, smtp.LocalName)

	msg := mail.NewMessage()
	msg.SetHeader("From", conf.Email.FromAddr)
	msg.SetHeader("Subject", fmt.Sprintf("vault-unseal: %s: %d errors occurred", conf.Environment, len(notifyQueue)))
	msg.SetBody("text/plain", text)

	msg.SetHeader("To", conf.Email.SendAddrs[0])
	if len(conf.Email.SendAddrs) > 1 {
		msg.SetHeader("CC", conf.Email.SendAddrs[1:]...)
	}

	if err := mail.Send(s, msg); err != nil {
		logger.WithError(err).Error("unable to send notification")
		return
	}

	logger.WithField("to", strings.Join(conf.Email.SendAddrs, ",")).Info("successfully sent notifications")
	notifyQueue = nil
}
