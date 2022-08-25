// Copyright (c) Liam Stanley <me@liamstanley.io>. All rights reserved. Use
// of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/apex/log"
)

func worker(ctx context.Context, wg *sync.WaitGroup, addr string) {
	defer wg.Done()

	client := newVault(addr)

	var errCount int
	var errDelay time.Duration

	for {
		errDelay = (30*time.Second)*time.Duration(errCount) + conf.CheckInterval
		if errDelay > conf.MaxCheckInterval {
			errDelay = conf.MaxCheckInterval
		}

		if errCount > 0 {
			logger.WithFields(log.Fields{
				"delay": errDelay,
				"addr":  addr,
			}).Info("delaying checks due to errors")
		}

		select {
		case <-ctx.Done():
			logger.WithField("addr", addr).Info("closing worker")
			return
		case <-time.After(conf.CheckInterval + errDelay):
			logger.WithField("addr", addr).Info("running checks")

			status, err := client.Sys().SealStatus()
			if err != nil {
				errCount++
				nerr := fmt.Errorf("checking seal status: %v", err)

				if err, ok := err.(net.Error); ok && err.Timeout() {
					// It's a network timeout. If it's the first network timeout,
					// don't notify, just try again. This should help with network
					// blips.

					if errCount == 1 {
						logger.WithField("addr", addr).WithError(err).Error("checking seal status")
						continue
					}
				}

				notify(nerr)
				continue
			}
			logger.WithFields(log.Fields{"addr": addr, "status": status}).Info("seal status")
			if !status.Sealed {
				// Not sealed, don't do anything.
				errCount = 0
				continue
			}

			// Attempt to loop through the tokens and send the unseal request.
			// Each vault-unseal service should have 2 keys. The API allows
			// unsealing from multiple locations, so as long as two nodes are
			// online, the unseal can occur using two keys from one instance, and
			// one key from another.

			for i, token := range conf.Tokens {
				logger.WithFields(log.Fields{
					"addr":     addr,
					"token":    i + 1,
					"progress": status.Progress,
					"total":    status.T,
				}).Info("using unseal token")
				resp, err := client.Sys().Unseal(token)
				if err != nil {
					notify(fmt.Errorf("using unseal key %d on %v: %v", i+1, addr, err))
					errCount++
					continue
				}

				logger.WithField("addr", addr).Info("token successfully sent")
				if !resp.Sealed {
					notify(fmt.Errorf("(was sealed) %v now unsealed with tokens", addr))
					continue
				}
			}
		}
	}
}
