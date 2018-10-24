// Copyright (c) Liam Stanley <me@liamstanley.io>. All rights reserved. Use
// of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	vapi "github.com/hashicorp/vault/api"
	flags "github.com/jessevdk/go-flags"
	"github.com/phayes/permbits"
	yaml "gopkg.in/yaml.v2"
)

var (
	version = "master"
	commit  = "latest"
	date    = "-"
)

type Flags struct {
	LogPath    string `short:"l" long:"log-path" description:"Optional path to log output to" value-name:"PATH"`
	ConfigPath string `short:"c" long:"config" description:"Path to configuration file" default:"./vault-unseal.yaml" value-name:"PATH"`
}

type Config struct {
	Environment string `yaml:"environment"`

	CheckInterval    time.Duration `yaml:"check_interval"`
	MaxCheckInterval time.Duration `yaml:"max_check_interval"`

	Nodes         []string `yaml:"vault_nodes"`
	TLSSkipVerify bool     `yaml:"tls_skip_verify"`
	Tokens        []string `yaml:"unseal_tokens"`

	NotifyMaxElapsed time.Duration `yaml:"notify_max_elapsed"`
	NotifyQueueDelay time.Duration `yaml:"notify_queue_delay"`

	Email struct {
		Enabled   bool     `yaml:"enabled"`
		Hostname  string   `yaml:"hostname"`
		Port      int      `yaml:"port"`
		Username  string   `yaml:"username"`
		Password  string   `yaml:"password"`
		FromAddr  string   `yaml:"from_addr"`
		SendAddrs []string `yaml:"send_addrs"`
	} `yaml:"email"`

	lastModifiedCheck time.Time
}

var (
	cli  = &Flags{}
	conf = &Config{
		CheckInterval: 30 * time.Second,
	}
	logger     = log.New(os.Stdout, "", log.Lshortfile|log.LstdFlags)
	httpClient = &http.Client{
		Timeout: 10 * time.Second,
	}
)

func newVault(addr string) (vault *vapi.Client) {
	vconfig := vapi.DefaultConfig()
	vconfig.Address = addr
	vconfig.MaxRetries = 0
	vconfig.Timeout = 15 * time.Second
	vconfig.ConfigureTLS(&vapi.TLSConfig{Insecure: conf.TLSSkipVerify})

	var err error
	if vault, err = vapi.NewClient(vconfig); err != nil {
		logger.Fatalf("error creating vault client: %v", err)
	}

	return vault
}

func main() {
	var err error
	if _, err = flags.Parse(cli); err != nil {
		if FlagErr, ok := err.(*flags.Error); ok && FlagErr.Type == flags.ErrHelp {
			os.Exit(0)
		}
		os.Exit(1)
	}

	if cli.LogPath != "" {
		logf, err := os.OpenFile(cli.LogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error opening log file %q: %v", cli.LogPath, err)
			os.Exit(1)
		}
		defer logf.Close()

		logger.SetOutput(io.MultiWriter(os.Stdout, logf))
	}

	if err := readConfig(cli.ConfigPath); err != nil {
		logger.Fatal(err)
	}

	var wg sync.WaitGroup
	done := make(chan struct{})

	for _, addr := range conf.Nodes {
		logger.Printf("invoking worker for %s", addr)
		wg.Add(1)
		go worker(done, &wg, addr)
	}

	go notifier(done, &wg)

	go func() {
		for {
			time.Sleep(15 * time.Second)

			if err := readConfig(cli.ConfigPath); err != nil {
				logger.Fatal(err)
			}
		}
	}()

	go func() {
		catch()
		close(done)
	}()

	wg.Wait()
}

func readConfig(path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}

	if perms := permbits.FileMode(fi.Mode()); perms != 0600 {
		logger.Fatalf("error: permissions of %q are insecure: %s, please use 0600", path, perms)
	}

	// Check to see if it's updated.
	if fi.ModTime() == conf.lastModifiedCheck {
		return nil
	}

	b, err := ioutil.ReadFile(path)
	if err != nil {
		return err
	}

	if err = yaml.Unmarshal(b, conf); err != nil {
		return err
	}

	if conf.CheckInterval < 5*time.Second {
		conf.CheckInterval = 5 * time.Second
	}
	if conf.MaxCheckInterval < conf.CheckInterval {
		// Default to 2x.
		conf.MaxCheckInterval = conf.CheckInterval * time.Duration(2)
	}

	if len(conf.Nodes) < 3 {
		logger.Fatal("error: not enough nodes in node list (must have at least 3!)")
	}

	if len(conf.Tokens) < 1 {
		logger.Fatal("error: no tokens found in config")
	}

	if len(conf.Tokens) >= 3 {
		logger.Printf("warning: found %d tokens in the config, make sure this is not a security risk", len(conf.Tokens))
	}

	if conf.Email.Enabled {
		if len(conf.Email.SendAddrs) < 1 {
			logger.Fatal("error: no send addresses setup for email")
		}
		if conf.Email.Hostname == "" || conf.Email.FromAddr == "" {
			logger.Fatal("error: email hostname or from address is empty")
		}
	}

	logger.Printf("updated config from %q", path)
	conf.lastModifiedCheck = fi.ModTime()

	return nil
}

func catch() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	<-signals
	fmt.Println("\ninvoked termination, cleaning up")
}
