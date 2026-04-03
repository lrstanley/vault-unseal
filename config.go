// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"slices"
	"strings"
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

type NotifyConfig struct {
	MaxElapsed time.Duration `name:"max-elapsed" env:"MAX_ELAPSED" help:"max time before the notification can be queued before it is sent" yaml:"max_elapsed"`
	QueueDelay time.Duration `name:"queue-delay" env:"QUEUE_DELAY" help:"time we queue the notification to allow as many notifications to be sent in one go (e.g. if no notification within X time, send all notifications)" yaml:"queue_delay"`
	URLs       []string      `name:"urls" env:"URLS" help:"shoutrrr URLs to send notifications to" yaml:"urls"`
}

type EmailConfig struct { // Deprecated: use notify.urls (Shoutrrr) instead.
	Enabled       bool     `name:"enabled"         env:"ENABLED"         hidden:"" yaml:"enabled"`
	Hostname      string   `name:"hostname"        env:"HOSTNAME"        hidden:"" yaml:"hostname"`
	Port          int      `name:"port"            env:"PORT"            hidden:"" yaml:"port"`
	Username      string   `name:"username"        env:"USERNAME"        hidden:"" yaml:"username"`
	Password      string   `name:"password"        env:"PASSWORD"        hidden:"" yaml:"password"`
	FromAddr      string   `name:"from-addr"       env:"FROM_ADDR"       hidden:"" yaml:"from_addr"`
	SendAddrs     []string `name:"send-addrs"      env:"SEND_ADDRS"      hidden:"" yaml:"send_addrs"`
	TLSSkipVerify bool     `name:"tls-skip-verify" env:"TLS_SKIP_VERIFY" hidden:"" yaml:"tls_skip_verify"`
	MandatoryTLS  bool     `name:"mandatory-tls"   env:"MANDATORY_TLS"   hidden:"" yaml:"mandatory_tls"`
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

	NotifyMaxElapsed time.Duration `name:"notify-max-elapsed" env:"-" hidden:"" yaml:"notify_max_elapsed"` // Deprecated: use notify.max_elapsed instead.
	NotifyQueueDelay time.Duration `name:"notify-queue-delay" env:"-" hidden:"" yaml:"notify_queue_delay"` // Deprecated: use notify.queue_delay instead.
	Notify           NotifyConfig  `embed:"" prefix:"notify." envprefix:"NOTIFY_" group:"Notify Options" yaml:"notify"`

	Email EmailConfig `embed:"" prefix:"email." envprefix:"EMAIL_" group:"Email Options" yaml:"email"` // Deprecated: use notify.urls (Shoutrrr) instead.

	TLSSkipVerifyLegacy bool      `name:"tls-skip-verify" env:"-" hidden:"" yaml:"tls_skip_verify"` // Deprecated: use tls.skip_verify instead.
	TLS                 TLSConfig `embed:"" prefix:"tls." envprefix:"TLS_" group:"TLS Options" yaml:"tls"`

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

		c.Notify.URLs = append(c.Notify.URLs, emailConfigShoutrrURL(c.Email).String())
	}

	slices.Sort(c.Notify.URLs)
	c.Notify.URLs = slices.Compact(c.Notify.URLs)
	for _, uri := range c.Notify.URLs {
		_, err := url.Parse(uri)
		if err != nil {
			return fmt.Errorf("invalid shoutrrr URL: %w", err)
		}
	}

	c.NotifyQueueDelay = clamp(c.NotifyQueueDelay, 10*time.Second, 10*time.Minute)

	return nil
}

func clamp[T cmp.Ordered](v, vmin, vmax T) T {
	if v < vmin {
		return vmin
	}
	if v > vmax {
		return vmax
	}
	return v
}

func emailConfigShoutrrURL(e EmailConfig) *url.URL {
	const implicitTLSPort = 465

	port := e.Port
	if port == 0 {
		port = 25
	}

	var userinfo *url.Userinfo
	switch {
	case e.Password != "":
		userinfo = url.UserPassword(e.Username, e.Password)
	case e.Username != "":
		userinfo = url.User(e.Username)
	}

	q := url.Values{}
	q.Set("fromAddress", e.FromAddr)
	q.Set("toAddresses", strings.Join(e.SendAddrs, ","))
	q.Set("clientHost", "auto")

	if e.Username != "" || e.Password != "" {
		q.Set("auth", "Plain")
	} else {
		q.Set("auth", "None")
	}

	switch {
	case e.MandatoryTLS && port == implicitTLSPort:
		q.Set("encryption", "ImplicitTLS")
	case e.MandatoryTLS:
		q.Set("encryption", "ExplicitTLS")
	default:
		q.Set("encryption", "Auto")
	}

	return &url.URL{
		Scheme:     "smtp",
		User:       userinfo,
		Host:       fmt.Sprintf("%s:%d", e.Hostname, port),
		Path:       "/",
		ForceQuery: true,
		RawQuery:   q.Encode(),
	}
}

func syncConfig(ctx context.Context, logger *slog.Logger) error {
	path := conf.Load().ConfigPath
	if path == "" {
		err := conf.Load().Validate(ctx, logger)
		if err != nil {
			return fmt.Errorf("error validating config: %w", err)
		}
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

	vlog.InfoContext(ctx, "detected config update")

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
		vlog.InfoContext(ctx, "config synced")
	}
	return nil
}
