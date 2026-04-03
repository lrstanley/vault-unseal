// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in the
// LICENSE file.

package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	vapi "github.com/hashicorp/vault/api"
	"github.com/lrstanley/clix/v2"
)

var (
	version = "master"
	commit  = "latest"
	date    = "-"

	conf atomic.Pointer[Config]
)

func main() {
	var exitCode int
	defer func() {
		os.Exit(exitCode)
	}()

	cli := clix.NewWithDefaults(
		clix.WithAppInfo[Config](clix.AppInfo{
			Name:        "vault-unseal",
			Description: "automatically unseals Hashicorp Vault clusters",
			Version:     version,
			Commit:      commit,
			Date:        date,
			Links:       clix.GithubLinks("github.com/lrstanley/vault-unseal", "master", ""),
		}),
	)
	conf.Store(cli.Flags)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	defer cancel()

	logger := cli.GetLogger()

	err := syncConfigFile(ctx, logger)
	if err != nil {
		logger.ErrorContext(ctx, "error reading config", "error", err)
		exitCode = 1
		return
	}

	err = conf.Load().Validate(ctx, logger)
	if err != nil {
		logger.ErrorContext(ctx, "error validating config", "error", err)
		exitCode = 1
		return
	}

	logger = logger.With(
		"environment", conf.Load().Environment,
	)

	var wg sync.WaitGroup

	for _, addr := range conf.Load().Nodes {
		var client *vapi.Client
		client, err = newVault(logger, addr)
		if err != nil {
			logger.ErrorContext(ctx, "error creating vault client", "err", err)
			exitCode = 1
			continue
		}

		logger.InfoContext(ctx, "invoking worker", "addr", addr)
		wg.Go(func() { worker(ctx, logger, client, addr) })
	}

	wg.Go(func() { notifier(ctx, logger) })

	if conf.Load().ConfigPath != "" {
		go func() {
			for {
				time.Sleep(configRefreshInterval)

				if rerr := syncConfigFile(ctx, logger); rerr != nil {
					logger.ErrorContext(ctx, "error reading config", "err", rerr)
					exitCode = 1
					cancel()
					return
				}
			}
		}()
	}

	wg.Wait()
}

func newVault(logger *slog.Logger, addr string) (*vapi.Client, error) {
	var err error

	vconfig := vapi.DefaultConfig()
	vconfig.Address = addr
	vconfig.MaxRetries = 0
	vconfig.Timeout = defaultTimeout
	vconfig.Logger = logger.With(
		"component", "vault-client",
		"addr", addr,
	)

	if err = vconfig.ConfigureTLS(&vapi.TLSConfig{Insecure: conf.Load().TLSSkipVerify}); err != nil {
		return nil, fmt.Errorf("error configuring tls config: %w", err)
	}

	var client *vapi.Client
	if client, err = vapi.NewClient(vconfig); err != nil {
		return nil, fmt.Errorf("error creating vault client: %w", err)
	}

	return client, nil
}
