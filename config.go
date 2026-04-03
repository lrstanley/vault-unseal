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

type TLSConfig struct {
	SkipVerify bool   `name:"skip-verify"  env:"SKIP_VERIFY"  help:"disables tls certificate validation: DO NOT DO THIS" yaml:"skip_verify"`
	ServerName string `name:"server-name"  env:"SERVER_NAME"  help:"server name to use for tls certificate validation" yaml:"server_name"`
	CACertPath string `name:"ca-cert-path" env:"CA_CERT_PATH" help:"path to the CA certificate file (takes precedence over ca-cert and ca-path)" yaml:"ca_cert_path"`
	CACert     string `name:"ca-cert"      env:"CA_CERT"      help:"CA certificate, pem encoded (takes precedence over ca-path)" yaml:"ca_cert"`
	CAPath     string `name:"ca-path"      env:"CA_PATH"      help:"path to the CA certificate directory" yaml:"ca_path"`
	ClientCert string `name:"client-cert"  env:"CLIENT_CERT"  help:"client certificate, pem encoded" yaml:"client_cert"`
	ClientKey  string `name:"client-key"   env:"CLIENT_KEY"   help:"client key, pem encoded" yaml:"client_key"`
}

type EmailConfig struct {
	Enabled       bool     `name:"enabled"         env:"ENABLED"         help:"enables email support" yaml:"enabled"`
	Hostname      string   `name:"hostname"        env:"HOSTNAME"        help:"hostname of mail server" yaml:"hostname"`
	Port          int      `name:"port"            env:"PORT"            help:"port of mail server" yaml:"port"`
	Username      string   `name:"username"        env:"USERNAME"        help:"username to authenticate to mail server" yaml:"username"`
	Password      string   `name:"password"        env:"PASSWORD"        help:"password to authenticate to mail server" yaml:"password"`
	FromAddr      string   `name:"from-addr"       env:"FROM_ADDR"       help:"address to use as 'From'" yaml:"from_addr"`
	SendAddrs     []string `name:"send-addrs"      env:"SEND_ADDRS"      help:"addresses to send notifications to" yaml:"send_addrs"`
	TLSSkipVerify bool     `name:"tls-skip-verify" env:"TLS_SKIP_VERIFY" help:"skip SMTP TLS certificate validation" yaml:"tls_skip_verify"`
	MandatoryTLS  bool     `name:"mandatory-tls"   env:"MANDATORY_TLS"   help:"require TLS for SMTP connections. Defaults to opportunistic." yaml:"mandatory_tls"`
}

// Config is a combo of the flags passed to the cli and the configuration file (if used).
type Config struct {
	ConfigPath string `name:"config" short:"c" env:"CONFIG_PATH" type:"path" help:"path to configuration file" placeholder:"PATH"`

	Environment string `name:"environment" env:"ENVIRONMENT" help:"environment this cluster relates to (for logging)" yaml:"environment"`

	CheckInterval    time.Duration `name:"check-interval"     env:"CHECK_INTERVAL"     help:"frequency of sealed checks against nodes" yaml:"check_interval"`
	MaxCheckInterval time.Duration `name:"max-check-interval" env:"MAX_CHECK_INTERVAL" help:"max time that vault-unseal will wait for an unseal check/attempt" yaml:"max_check_interval"`

	AllowSingleNode bool     `name:"allow-single-node" env:"ALLOW_SINGLE_NODE" help:"allow vault-unseal to run on a single node" yaml:"allow_single_node"`
	Nodes           []string `name:"nodes"             env:"NODES"             help:"nodes to connect/provide tokens to" yaml:"vault_nodes"`
	Tokens          []string `name:"tokens"            env:"TOKENS"            help:"tokens to provide to nodes" yaml:"unseal_tokens"`

	NotifyMaxElapsed time.Duration `name:"notify-max-elapsed" env:"NOTIFY_MAX_ELAPSED" help:"max time before the notification can be queued before it is sent" yaml:"notify_max_elapsed"`
	NotifyQueueDelay time.Duration `name:"notify-queue-delay" env:"NOTIFY_QUEUE_DELAY" help:"time we queue the notification to allow as many notifications to be sent in one go (e.g. if no notification within X time, send all notifications)" yaml:"notify_queue_delay"`

	TLSSkipVerifyLegacy bool      `name:"tls-skip-verify" env:"-" hidden:"" yaml:"tls_skip_verify"` // Deprecated: use tls.skip_verify instead.
	TLS                 TLSConfig `embed:"" prefix:"tls." envprefix:"TLS_" group:"TLS Options" yaml:"tls"`

	Email EmailConfig `embed:"" prefix:"email." envprefix:"EMAIL_" group:"Email Options" yaml:"email"`

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
