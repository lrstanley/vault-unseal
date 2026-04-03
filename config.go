// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/goccy/go-yaml"
)

const (
	defaultCheckInterval  = 30 * time.Second
	defaultTimeout        = 15 * time.Second
	configRefreshInterval = 15 * time.Second
	minimumNodes          = 3
)

// Config is a combo of the flags passed to the cli and the configuration file (if used).
type Config struct {
	ConfigPath string `name:"config" short:"c" env:"CONFIG_PATH" type:"path" help:"path to configuration file" placeholder:"PATH"`

	Environment string `name:"environment" env:"ENVIRONMENT" help:"environment this cluster relates to (for logging)" yaml:"environment"`

	CheckInterval    time.Duration `name:"check-interval" env:"CHECK_INTERVAL" help:"frequency of sealed checks against nodes" yaml:"check_interval"`
	MaxCheckInterval time.Duration `name:"max-check-interval" env:"MAX_CHECK_INTERVAL" help:"max time that vault-unseal will wait for an unseal check/attempt" yaml:"max_check_interval"`

	AllowSingleNode bool     `name:"allow-single-node" env:"ALLOW_SINGLE_NODE" help:"allow vault-unseal to run on a single node" yaml:"allow_single_node"`
	Nodes           []string `name:"nodes" env:"NODES" help:"nodes to connect/provide tokens to" yaml:"vault_nodes"`
	TLSSkipVerify   bool     `name:"tls-skip-verify" env:"TLS_SKIP_VERIFY" help:"disables tls certificate validation: DO NOT DO THIS" yaml:"tls_skip_verify"`
	Tokens          []string `name:"tokens" env:"TOKENS" help:"tokens to provide to nodes" yaml:"unseal_tokens"`

	NotifyMaxElapsed time.Duration `name:"notify-max-elapsed" env:"NOTIFY_MAX_ELAPSED" help:"max time before the notification can be queued before it is sent" yaml:"notify_max_elapsed"`
	NotifyQueueDelay time.Duration `name:"notify-queue-delay" env:"NOTIFY_QUEUE_DELAY" help:"time we queue the notification to allow as many notifications to be sent in one go (e.g. if no notification within X time, send all notifications)" yaml:"notify_queue_delay"`

	Email struct {
		Enabled       bool     `name:"enabled" env:"EMAIL_ENABLED" help:"enables email support" yaml:"enabled"`
		Hostname      string   `name:"hostname" env:"EMAIL_HOSTNAME" help:"hostname of mail server" yaml:"hostname"`
		Port          int      `name:"port" env:"EMAIL_PORT" help:"port of mail server" yaml:"port"`
		Username      string   `name:"username" env:"EMAIL_USERNAME" help:"username to authenticate to mail server" yaml:"username"`
		Password      string   `name:"password" env:"EMAIL_PASSWORD" help:"password to authenticate to mail server" yaml:"password"`
		FromAddr      string   `name:"from-addr" env:"EMAIL_FROM_ADDR" help:"address to use as 'From'" yaml:"from_addr"`
		SendAddrs     []string `name:"send-addrs" env:"EMAIL_SEND_ADDRS" help:"addresses to send notifications to" yaml:"send_addrs"`
		TLSSkipVerify bool     `name:"tls-skip-verify" env:"EMAIL_TLS_SKIP_VERIFY" help:"skip SMTP TLS certificate validation" yaml:"tls_skip_verify"`
		MandatoryTLS  bool     `name:"mandatory-tls" env:"EMAIL_MANDATORY_TLS" help:"require TLS for SMTP connections. Defaults to opportunistic." yaml:"mandatory_tls"`
	} `embed:"" prefix:"email." group:"Email Options" yaml:"email"`

	lastModifiedCheck time.Time `kong:"-" yaml:"-"`
}

func (c *Config) Validate(ctx context.Context, logger *slog.Logger) error {
	vlog := logger.With("component", "config-validator")

	if c.CheckInterval <= 0 {
		c.CheckInterval = defaultCheckInterval
	}

	c.CheckInterval = max(c.CheckInterval, 5*time.Second)

	if c.MaxCheckInterval < c.CheckInterval {
		// Default to 2x.
		c.MaxCheckInterval = c.CheckInterval * time.Duration(2)
	}

	if len(c.Nodes) < minimumNodes {
		if !c.AllowSingleNode {
			return fmt.Errorf("not enough nodes in node list (must have at least %d)", minimumNodes)
		}

		vlog.WarnContext(
			ctx, "running with less than minimum nodes, this is not recommended",
			"minimum", minimumNodes,
		)
	}

	if len(c.Tokens) < 1 {
		return errors.New("no tokens found in config")
	}

	if len(c.Tokens) >= 3 {
		vlog.WarnContext(
			ctx, "found multiple tokens in the config, make sure this is not a security risk",
			"count", len(c.Tokens),
		)
	}

	if c.Email.Enabled {
		if len(c.Email.SendAddrs) < 1 {
			return errors.New("no send addresses setup for email")
		}
		if c.Email.Hostname == "" || c.Email.FromAddr == "" {
			return errors.New("email hostname or from address is empty")
		}
	}

	c.NotifyQueueDelay = min(max(c.NotifyQueueDelay, 10*time.Second), 10*time.Minute)

	return nil
}

func syncConfigFile(ctx context.Context, logger *slog.Logger) error {
	path := conf.Load().ConfigPath
	if path == "" {
		return nil
	}
	lastModifiedCheck := conf.Load().lastModifiedCheck

	vlog := logger.With(
		"component", "config-syncer",
		"path", path,
	)

	stat, err := os.Stat(path)
	if err != nil {
		return err
	}

	if perms := stat.Mode().Perm(); perms != 0o600 && perms != 0o440 && perms != 0o400 {
		return fmt.Errorf(
			"permissions of %q are insecure: %s, please use 0600, 0440, or 0400",
			path, perms,
		)
	}

	// Check to see if it's updated.
	if stat.ModTime().Equal(lastModifiedCheck) {
		return nil
	}

	var b []byte
	b, err = os.ReadFile(path)
	if err != nil {
		return err
	}

	newConfig := *conf.Load()

	err = yaml.Unmarshal(b, &newConfig) //nolint:musttag
	if err != nil {
		return err
	}

	err = newConfig.Validate(ctx, vlog)
	if err != nil {
		return fmt.Errorf("error validating config from file: %w", err)
	}

	newConfig.lastModifiedCheck = stat.ModTime()
	conf.Store(&newConfig)

	if !lastModifiedCheck.IsZero() {
		vlog.InfoContext(ctx, "updated config")
	}
	return nil
}
