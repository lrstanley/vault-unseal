// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
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
	"strings"
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
	yaml "gopkg.in/yaml.v3"
	core "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

var (
	version = "master"
	commit  = "latest"
	date    = "-"
)

const (
	defaultVaultName          = "vault"
	defaultCheckInterval      = 30 * time.Second
	defaultTimeout            = 15 * time.Second
	configRefreshInterval     = 15 * time.Second
	defaultMonitoringInterval = configRefreshInterval / 2
	minimumNodes              = 3

	appNameLabel  = "app.kubernetes.io/name"
	instanceLabel = "app.kubernetes.io/instance"
)

// Config is a combo of the flags passed to the cli and the configuration file (if used).
type Config struct {
	Version    bool   `short:"v" long:"version" description:"display the version of vault-unseal and exit"`
	Debug      bool   `short:"D" long:"debug" description:"enable debugging (extra logging)"`
	ConfigPath string `env:"CONFIG_PATH" short:"c" long:"config" description:"path to configuration file" value-name:"PATH"`

	Log struct {
		Path   string `env:"LOG_PATH"  long:"path"    description:"path to log output to" value-name:"PATH"`
		Quiet  bool   `env:"LOG_QUIET" long:"quiet"   description:"disable logging to stdout (also: see levels)"`
		Level  string `env:"LOG_LEVEL" long:"level"   default:"info" choice:"debug" choice:"info" choice:"warn" choice:"error" choice:"fatal"  description:"logging level"`
		JSON   bool   `env:"LOG_JSON"   long:"json"   description:"output logs in JSON format"`
		Pretty bool   `env:"LOG_PRETTY" long:"pretty" description:"output logs in a pretty colored format (cannot be easily parsed)"`
	} `group:"Logging Options" namespace:"log"`

	Environment string `env:"ENVIRONMENT" long:"environment" description:"environment this cluster relates to (for logging)" yaml:"environment"`

	CheckInterval    time.Duration `env:"CHECK_INTERVAL"     long:"check-interval" description:"frequency of sealed checks against nodes" yaml:"check_interval"`
	MaxCheckInterval time.Duration `env:"MAX_CHECK_INTERVAL" long:"max-check-interval" description:"max time that vault-unseal will wait for an unseal check/attempt" yaml:"max_check_interval"`

	AllowSingleNode bool     `env:"ALLOW_SINGLE_NODE" long:"allow-single-node"    description:"allow vault-unseal to run on a single node" yaml:"allow_single_node" hidden:"true"`
	Nodes           []string `env:"NODES"             long:"nodes" env-delim:","  description:"nodes to connect/provide tokens to (can be provided multiple times & uses comma-separated string for environment variable)" yaml:"vault_nodes"`
	TLSSkipVerify   bool     `env:"TLS_SKIP_VERIFY"   long:"tls-skip-verify"      description:"disables tls certificate validation: DO NOT DO THIS" yaml:"tls_skip_verify"`
	Tokens          []string `env:"TOKENS"            long:"tokens" env-delim:"," description:"tokens to provide to nodes (can be provided multiple times & uses comma-separated string for environment variable)" yaml:"unseal_tokens"`
	VaultService    string   `env:"VAULT_SERVICE"     long:"vault-service" env-delim:"," description:"service to get vault pods from" yaml:"vault_service"`

	NotifyMaxElapsed time.Duration `env:"NOTIFY_MAX_ELAPSED" long:"notify-max-elapsed" description:"max time before the notification can be queued before it is sent" yaml:"notify_max_elapsed"`
	NotifyQueueDelay time.Duration `env:"NOTIFY_QUEUE_DELAY" long:"notify-queue-delay" description:"time we queue the notification to allow as many notifications to be sent in one go (e.g. if no notification within X time, send all notifications)" yaml:"notify_queue_delay"`

	Email struct {
		Enabled       bool     `env:"EMAIL_ENABLED"         long:"enabled"         description:"enables email support" yaml:"enabled"`
		Hostname      string   `env:"EMAIL_HOSTNAME"        long:"hostname"        description:"hostname of mail server" yaml:"hostname"`
		Port          int      `env:"EMAIL_PORT"            long:"port"            description:"port of mail server" yaml:"port"`
		Username      string   `env:"EMAIL_USERNAME"        long:"username"        description:"username to authenticate to mail server" yaml:"username"`
		Password      string   `env:"EMAIL_PASSWORD"        long:"password"        description:"password to authenticate to mail server" yaml:"password"`
		FromAddr      string   `env:"EMAIL_FROM_ADDR"       long:"from-addr"       description:"address to use as 'From'" yaml:"from_addr"`
		SendAddrs     []string `env:"EMAIL_SEND_ADDRS"      long:"send-addrs"      description:"addresses to send notifications to" yaml:"send_addrs"`
		TLSSkipVerify bool     `env:"EMAIL_TLS_SKIP_VERIFY" long:"tls-skip-verify" description:"skip SMTP TLS certificate validation" yaml:"tls_skip_verify"`
		MandatoryTLS  bool     `env:"EMAIL_MANDATORY_TLS"   long:"mandatory-tls"   description:"require TLS for SMTP connections. Defaults to opportunistic." yaml:"mandatory_tls"`
	} `group:"Email Options" namespace:"email" yaml:"email"`

	lastModifiedCheck time.Time
}

var (
	conf = &Config{CheckInterval: defaultCheckInterval}

	logger log.Interface
)

func newVault(addr string) (vault *vapi.Client) {
	var err error

	vconfig := vapi.DefaultConfig()
	vconfig.Address = addr
	vconfig.MaxRetries = 0
	vconfig.Timeout = defaultTimeout

	if err = vconfig.ConfigureTLS(&vapi.TLSConfig{Insecure: conf.TLSSkipVerify}); err != nil {
		logger.WithError(err).Fatal("error initializing tls config")
	}

	if vault, err = vapi.NewClient(vconfig); err != nil {
		logger.Fatalf("error creating vault client: %v", err)
	}

	return vault
}

// getKubeClient returns a kubernetes clientset.
//
// This function will attempt to get the in-cluster config. The ServiceAccount requires the
// following permissions:
// - `get` on `services` in the `default` namespace
// - `get` on `pods` in the `default` namespace
func getKubeClient() (*kubernetes.Clientset, error) {
	kubeconfig, err := rest.InClusterConfig()
	if err != nil {
		logger.WithError(err).Warn("error getting in-cluster config, falling back to kubeconfig")
		os.Exit(1)
	}

	client := new(kubernetes.Clientset)
	if client, err = kubernetes.NewForConfig(kubeconfig); err != nil {
		logger.WithError(err).Fatal("error creating kubernetes client")
	}

	return client, nil
}

func getVaultPodsForService() ([]string, error) {
	client, err := getKubeClient()
	if err != nil {
		return nil, fmt.Errorf("error getting kubernetes client: %w", err)
	}

	services, err := client.CoreV1().Services(defaultVaultName).List(context.TODO(), metav1.ListOptions{
		LabelSelector: labels.Set{
			appNameLabel: defaultVaultName,
		}.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("error getting services: %w", err)
	}

	if len(services.Items) == 0 {
		return nil, errors.New("no services found")
	}

	podAddrs := make([]string, 0)
	for _, service := range services.Items {
		pods, err := client.CoreV1().Pods(defaultVaultName).List(context.TODO(), metav1.ListOptions{
			LabelSelector: labels.Set{
				appNameLabel:  defaultVaultName,
				instanceLabel: service.Name,
			}.String(),
		})
		if err != nil {
			return nil, fmt.Errorf("error getting pods for service %q: %w", service.Name, err)
		}

		for _, pod := range pods.Items {
			podAddrs = append(podAddrs, getVaultAddr(service.Spec.Ports, pod.Status.PodIP))
		}
	}

	return podAddrs, nil
}

func getVaultAddr(service []core.ServicePort, ip string) string {
	const targetScheme = "http"

	for _, port := range service {
		if port.Name == targetScheme {
			return fmt.Sprintf("%s://%s:%d", port.Protocol, ip, port.Port)
		}
	}

	return fmt.Sprintf("%s://%s:8200", targetScheme, ip) // Default to 8200 on http.
}

// monitorService is a function that will monitor a service for changes to the pods attached to it.
func monitorService(ctx context.Context, workerIps *sync.Map, wg *sync.WaitGroup) {
	client, err := getKubeClient()
	if err != nil {
		logger.WithError(err).Fatal("error getting kubernetes client")
	}

	ticker := time.NewTicker(defaultMonitoringInterval)

	for range ticker.C {
		select {
		case <-ctx.Done():
			logger.Info("closing service monitor")
			return
		default:
			services, err := client.CoreV1().Services(defaultVaultName).List(context.TODO(), metav1.ListOptions{
				LabelSelector: labels.Set{
					appNameLabel: defaultVaultName,
				}.String(),
			})
			if err != nil {
				logger.WithError(err).Error("error getting services")
				continue
			}

			if len(services.Items) == 0 {
				logger.Warn("no services found")
				continue
			}

			fmt.Println(workerIps)

			svcName := strings.Split(conf.VaultService, ".")[1]
			for _, service := range services.Items {
				if service.Name != svcName {
					continue
				}

				pods, err := client.CoreV1().Pods(defaultVaultName).List(context.TODO(), metav1.ListOptions{
					LabelSelector: labels.Set{
						appNameLabel:  defaultVaultName,
						instanceLabel: service.Name,
					}.String(),
				})
				if err != nil {
					logger.WithError(err).Errorf("error getting pods for service %q", service.Name)
					continue
				}

				// Check for new pods.
				for _, pod := range pods.Items {
					addr := getVaultAddr(service.Spec.Ports, pod.Status.PodIP)
					if _, ok := workerIps.Load(addr); !ok {
						logger.WithField("addr", addr).Info("adding worker")
						wg.Add(1)
						workerCtx, workerCancel := context.WithCancel(ctx)
						workerIps.Store(addr, workerCancel)
						go worker(workerCtx, wg, addr)
					}
				}

				// Check for removed pods.
				workerIps.Range(func(key, value interface{}) bool {
					addr, ok := key.(string)
					if !ok {
						// This should never happen. Do not stop the loop.
						return true
					}

					found := false
					for _, pod := range pods.Items {
						podAddr := getVaultAddr(service.Spec.Ports, pod.Status.PodIP)
						if addr == podAddr {
							found = true
							break
						}
					}

					if !found {
						logger.WithField("addr", addr).Info("removing worker")
						value.(context.CancelFunc)()
						workerIps.Delete(addr)
					}

					return true
				})
			}

			fmt.Println(workerIps)
		}
	}
}

func main() {
	var err error
	if _, err = flags.Parse(conf); err != nil {
		var ferr *flags.Error
		if errors.As(err, &ferr) && errors.Is(ferr.Type, flags.ErrHelp) {
			os.Exit(0)
		}
		os.Exit(1)
	}

	if conf.Version {
		fmt.Printf("vault-unseal version: %s [%s] (%s, %s), compiled %s\n", version, commit, runtime.GOOS, runtime.GOARCH, date) //nolint:forbidigo
		os.Exit(0)
	}

	// Initialize logging.
	initLogger := &log.Logger{}
	if conf.Debug {
		initLogger.Level = log.DebugLevel
	} else {
		initLogger.Level = log.MustParseLevel(conf.Log.Level)
	}

	logWriters := make([]io.Writer, 0)

	if conf.Log.Path != "" {
		var logFileWriter *os.File
		logFileWriter, err = os.OpenFile(conf.Log.Path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o666)
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "error opening log file %q: %v", conf.Log.Path, err)
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

	if len(conf.Nodes) == 0 {
		conf.Nodes = make([]string, 0)
	}

	// Get the vault pods that are attached to the given service.
	if conf.VaultService != "" {
		podAddrs, err := getVaultPodsForService()
		if err != nil {
			logger.WithError(err).Fatal("error getting vault pods")
		} else if len(podAddrs) > 0 {
			conf.Nodes = append(conf.Nodes, podAddrs...)
		}
	}

	workers := new(sync.Map)
	for _, addr := range conf.Nodes {
		logger.WithField("addr", addr).Info("invoking worker")
		wg.Add(1)
		workerCtx, workerCancel := context.WithCancel(ctx)
		workers.Store(addr, workerCancel)
		go worker(workerCtx, &wg, addr)
	}

	if conf.VaultService != "" {
		go monitorService(ctx, workers, &wg)
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

		if perms := fi.Mode().Perm(); perms != 0o600 && perms != 0o400 && perms != 0o440 {
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

		err = yaml.Unmarshal(b, conf) //nolint:musttag
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

	if len(conf.Nodes) < minimumNodes && conf.VaultService == "" {
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
