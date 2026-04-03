// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/hashicorp/vault/api"
)

func worker(ctx context.Context, logger *slog.Logger, cancel context.CancelFunc, addr string) {
	var errCount int
	var errDelay time.Duration

	wlog := logger.With(
		"component", "worker",
		"addr", addr,
	)

	for {
		client, err := newVault(logger, addr)
		if err != nil {
			logger.ErrorContext(ctx, "error creating vault client", "err", err)
			cancel()
			return
		}

		errDelay = min((30*time.Second)*time.Duration(errCount), conf.Load().MaxCheckInterval)

		if errCount > 0 {
			wlog.InfoContext(ctx, "delaying checks due to errors", "delay", errDelay)
		}

		select {
		case <-ctx.Done():
			wlog.InfoContext(ctx, "closing worker")
			return
		case <-time.After(conf.Load().CheckInterval + errDelay):
			wlog.InfoContext(ctx, "running checks")

			var status *api.SealStatusResponse
			status, err = client.Sys().SealStatus()
			if err != nil {
				errCount++
				nerr := fmt.Errorf("checking seal status: %w", err)

				if nerr, ok := errors.AsType[net.Error](err); ok && nerr.Timeout() {
					// It's a network timeout. If it's the first network timeout,
					// don't notify, just try again. This should help with network
					// blips.

					if errCount == 1 {
						wlog.ErrorContext(
							ctx, "checking seal status",
							"error", err,
							"timeout", true,
						)
						continue
					}
				}

				notify(ctx, wlog, nerr)
				continue
			}
			wlog.InfoContext(ctx, "seal status", "status", status)
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

			for i, token := range conf.Load().Tokens {
				wlog.InfoContext(
					ctx, "using unseal token",
					"token", i+1,
					"progress", status.Progress,
					"total", status.T,
				)

				var resp *api.SealStatusResponse
				resp, err = client.Sys().Unseal(token)
				if err != nil {
					notify(ctx, wlog, fmt.Errorf("using unseal key %d on %v: %w", i+1, addr, err))
					errCount++
					continue
				}

				wlog.InfoContext(ctx, "token successfully sent")
				if !resp.Sealed {
					notify(ctx, wlog, fmt.Errorf("(was sealed) %v now unsealed with tokens", addr))
					continue
				}
			}
		}
	}
}
