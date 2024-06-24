// Copyright (c) Liam Stanley <me@liamstanley.io>. All rights reserved. Use
// of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/apex/log"
	"github.com/apex/log/handlers/json"
	"github.com/apex/log/handlers/logfmt"
	"github.com/apex/log/handlers/text"
	vapi "github.com/hashicorp/vault/api"
	flags "github.com/jessevdk/go-flags"
	_ "github.com/joho/godotenv/autoload"
	"github.com/mitchellh/mapstructure"
	"github.com/phayes/permbits"
	yaml "gopkg.in/yaml.v2"
)

var (
	version = "master"
	commit  = "latest"
	date    = "-"
)

const (
	defaultCheckInterval  = 30 * time.Second
	defaultTimeout        = 15 * time.Second
	configRefreshInterval = 15 * time.Second
	minimumNodes          = 3
)

// Config is a combo of the flags passed to the cli and the configuration file (if used).
type Config struct {
	Version    bool   `short:"v" long:"version" description:"display the version of vault-unseal and exit"`
	Debug      bool   `short:"D" long:"debug" description:"enable debugging (extra logging)"`
	ConfigPath string `env:"CONFIG_PATH" short:"c" long:"config" description:"path to configuration file" value-name:"PATH"`

	Log struct {
		Path   string `env:"LOG_PATH" long:"path" description:"path to log output to" value-name:"PATH"`
		Quiet  bool   `env:"LOG_QUIET" long:"quiet" description:"disable logging to stdout (also: see levels)"`
		Level  string `env:"LOG_LEVEL" long:"level" default:"info" choice:"debug" choice:"info" choice:"warn" choice:"error" choice:"fatal"  description:"logging level"`
		JSON   bool   `env:"LOG_JSON" long:"json" description:"output logs in JSON format"`
		Pretty bool   `env:"LOG_PRETTY" long:"pretty" description:"output logs in a pretty colored format (cannot be easily parsed)"`
	} `group:"Logging Options" namespace:"log"`

	Environment string `env:"ENVIRONMENT" long:"environment" description:"environment this cluster relates to (for logging)" yaml:"environment"`

	CheckInterval    time.Duration `env:"CHECK_INTERVAL" long:"check-interval" description:"frequency of sealed checks against nodes" yaml:"check_interval"`
	MaxCheckInterval time.Duration `env:"MAX_CHECK_INTERVAL" long:"max-check-interval" description:"max time that vault-unseal will wait for an unseal check/attempt" yaml:"max_check_interval"`

	AllowSingleNode bool     `env:"ALLOW_SINGLE_NODE" long:"allow-single-node" description:"allow vault-unseal to run on a single node" yaml:"allow_single_node" hidden:"true"`
	Nodes           []string `env:"NODES" long:"nodes" env-delim:"," description:"nodes to connect/provide tokens to (can be provided multiple times & uses comma-separated string for environment variable)" yaml:"vault_nodes"`

	TLS struct {
		CACert        string `env:"TLS_CA_CERT" long:"ca-cert" description:"the path to a PEM-encoded CA cert file to use to verify the Vault server SSL certificate. It takes precedence over CACertBytes and CAPath" yaml:"ca_cert"`
		CACertBytes   []byte `env:"TLS_CA_CERT_BYTES" long:"ca-cert-bytes" description:"PEM-encoded certificate or bundle. It takes precedence over CAPath" yaml:"ca_cert_bytes"`
		CAPath        string `env:"TLS_CA_PATH" long:"ca-path" description:"path to a directory of PEM-encoded CA cert files to verify the Vault server SSL certificate" yaml:"ca_path"`
		ClientCert    string `env:"TLS_CLIENT_CERT" long:"client-cert" description:"path to the certificate for Vault communication" yaml:"client_path"`
		ClientKey     string `env:"TLS_CLIENT_KEY" long:"client-key" description:"path to the private key for Vault communication" yaml:"client_key"`
		TLSServerName string `env:"TLS_SERVER_NAME" long:"server-name" description:"if set, is used to set the SNI host when connecting via TLS" yaml:"server_name"`
		Insecure      bool   `env:"TLS_INSECURE" long:"insecure" description:"enables or disables SSL verification: DO NOT DO THIS" yaml:"insecure"`
	} `group:"TLS Options" namespace:"tls" yaml:"tls"`

	Tokens []string `env:"TOKENS" long:"tokens" env-delim:"," description:"tokens to provide to nodes (can be provided multiple times & uses comma-separated string for environment variable)" yaml:"unseal_tokens"`

	NotifyMaxElapsed time.Duration `env:"NOTIFY_MAX_ELAPSED" long:"notify-max-elapsed" description:"max time before the notification can be queued before it is sent" yaml:"notify_max_elapsed"`
	NotifyQueueDelay time.Duration `env:"NOTIFY_QUEUE_DELAY" long:"notify-queue-delay" description:"time we queue the notification to allow as many notifications to be sent in one go (e.g. if no notification within X time, send all notifications)" yaml:"notify_queue_delay"`

	Email struct {
		Enabled       bool     `env:"EMAIL_ENABLED" long:"enabled" description:"enables email support" yaml:"enabled"`
		Hostname      string   `env:"EMAIL_HOSTNAME" long:"hostname" description:"hostname of mail server" yaml:"hostname"`
		Port          int      `env:"EMAIL_PORT" long:"port" description:"port of mail server" yaml:"port"`
		Username      string   `env:"EMAIL_USERNAME" long:"username" description:"username to authenticate to mail server" yaml:"username"`
		Password      string   `env:"EMAIL_PASSWORD" long:"password" description:"password to authenticate to mail server" yaml:"password"`
		FromAddr      string   `env:"EMAIL_FROM_ADDR" long:"from-addr" description:"address to use as 'From'" yaml:"from_addr"`
		SendAddrs     []string `env:"EMAIL_SEND_ADDRS" long:"send-addrs" description:"addresses to send notifications to" yaml:"send_addrs"`
		TLSSkipVerify bool     `env:"EMAIL_TLS_SKIP_VERIFY" long:"tls-skip-verify" description:"skip SMTP TLS certificate validation" yaml:"tls_skip_verify"`
		MandatoryTLS  bool     `env:"EMAIL_MANDATORY_TLS" long:"mandatory-tls" description:"require TLS for SMTP connections. Defaults to opportunistic." yaml:"mandatory_tls"`
	} `group:"Email Options" namespace:"email" yaml:"email"`

	lastModifiedCheck time.Time
}

var (
	conf = &Config{CheckInterval: defaultCheckInterval}

	logger log.Interface
)

func ConvertTLSConfigToTLS(src Config, dest *vapi.TLSConfig) error {
	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		Result: dest,
	})
	if err != nil {
		return err
	}

	return decoder.Decode(&src.TLS)
}

func newVault(addr string) (vault *vapi.Client) {
	var err error

	vconfig := vapi.DefaultConfig()
	vconfig.Address = addr
	vconfig.MaxRetries = 0
	vconfig.Timeout = defaultTimeout

	var tlsConfigDest vapi.TLSConfig

	errConvertTLS := ConvertTLSConfigToTLS(*conf, &tlsConfigDest)
	if errConvertTLS != nil {
		logger.Fatalf("error configuring vault client TLS: %v", errConvertTLS)
	}

	if err = vconfig.ConfigureTLS(&tlsConfigDest); err != nil {
		logger.WithError(err).Fatal("error initializing tls config")
	}

	if vault, err = vapi.NewClient(vconfig); err != nil {
		logger.Fatalf("error creating vault client: %v", err)
	}

	return vault
}

func main() {
	var err error
	if _, err = flags.Parse(conf); err != nil {
		var ferr *flags.Error
		if errors.As(err, &ferr) && ferr.Type == flags.ErrHelp {
			os.Exit(0)
		}
		os.Exit(1)
	}

	if conf.Version {
		fmt.Printf("vault-unseal version: %s [%s] (%s, %s), compiled %s\n", version, commit, runtime.GOOS, runtime.GOARCH, date)
		os.Exit(0)
	}

	// Initialize logging.
	initLogger := &log.Logger{}
	if conf.Debug {
		initLogger.Level = log.DebugLevel
	} else {
		initLogger.Level = log.MustParseLevel(conf.Log.Level)
	}

	logWriters := []io.Writer{}

	if conf.Log.Path != "" {
		var logFileWriter *os.File
		logFileWriter, err = os.OpenFile(conf.Log.Path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o666)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error opening log file %q: %v", conf.Log.Path, err)
			os.Exit(1)
		}
		defer logFileWriter.Close()

		logWriters = append(logWriters, logFileWriter)
	}

	if !conf.Log.Quiet {
		logWriters = append(logWriters, os.Stdout)
	} else {
		logWriters = append(logWriters, io.Discard)
	}

	if conf.Log.JSON {
		initLogger.Handler = json.New(io.MultiWriter(logWriters...))
	} else if conf.Log.Pretty {
		initLogger.Handler = text.New(io.MultiWriter(logWriters...))
	} else {
		initLogger.Handler = logfmt.New(io.MultiWriter(logWriters...))
	}

	logger = initLogger.WithFields(log.Fields{
		"environment": conf.Environment,
		"version":     version,
	})

	err = readConfig(conf.ConfigPath)
	if err != nil {
		logger.WithError(err).Fatal("error reading config")
	}

	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())

	for _, addr := range conf.Nodes {
		logger.WithField("addr", addr).Info("invoking worker")
		wg.Add(1)
		go worker(ctx, &wg, addr)
	}

	go notifier(ctx, &wg)

	if conf.ConfigPath != "" {
		go func() {
			for {
				time.Sleep(configRefreshInterval)

				err = readConfig(conf.ConfigPath)
				if err != nil {
					logger.WithError(err).Fatal("error reading config")
				}
			}
		}()
	}

	go func() {
		catch()
		cancel()
	}()

	wg.Wait()
}

func readConfig(path string) error {
	var err error
	var fi os.FileInfo

	if path != "" {
		fi, err = os.Stat(path)
		if err != nil {
			return err
		}

		if perms := permbits.FileMode(fi.Mode()); perms != 0o600 && perms != 0o400 && perms != 0o440 {
			return fmt.Errorf("permissions of %q are insecure: %s, please use 0600, 0440, or 0400", path, perms)
		}

		// Check to see if it's updated.
		if fi.ModTime() == conf.lastModifiedCheck {
			return nil
		}

		var b []byte
		b, err = os.ReadFile(path)
		if err != nil {
			return err
		}

		err = yaml.Unmarshal(b, conf)
		if err != nil {
			return err
		}
	}

	if conf.CheckInterval < 5*time.Second {
		conf.CheckInterval = 5 * time.Second
	}
	if conf.MaxCheckInterval < conf.CheckInterval {
		// Default to 2x.
		conf.MaxCheckInterval = conf.CheckInterval * time.Duration(2)
	}

	if len(conf.Nodes) < minimumNodes {
		if !conf.AllowSingleNode {
			return fmt.Errorf("not enough nodes in node list (must have at least %d)", minimumNodes)
		}

		logger.Warnf("running with less than %d nodes, this is not recommended", minimumNodes)
	}

	if len(conf.Tokens) < 1 {
		return errors.New("no tokens found in config")
	}

	if len(conf.Tokens) >= 3 {
		logger.Warnf("found %d tokens in the config, make sure this is not a security risk", len(conf.Tokens))
	}

	if conf.Email.Enabled {
		if len(conf.Email.SendAddrs) < 1 {
			return errors.New("no send addresses setup for email")
		}
		if conf.Email.Hostname == "" || conf.Email.FromAddr == "" {
			return errors.New("email hostname or from address is empty")
		}
	}

	if conf.NotifyQueueDelay < 10*time.Second {
		conf.NotifyQueueDelay = 10 * time.Second
	}
	if conf.NotifyQueueDelay > 10*time.Minute {
		conf.NotifyQueueDelay = 10 * time.Minute
	}

	if path != "" {
		logger.WithField("path", path).Info("updated config")
		conf.lastModifiedCheck = fi.ModTime()
	}

	return nil
}

func catch() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	<-signals
	logger.Info("invoked termination, cleaning up")
}
