// Copyright (c) Liam Stanley <me@liamstanley.io>. All rights reserved. Use
// of this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/apex/log"
	"github.com/hashicorp/vault/api"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

type KubernetesConfig struct {
	Enabled    bool   `env:"KUBERNETES_ENABLED" long:"enabled" description:"enables kubernetes mode" yaml:"enabled"`
	Kubeconfig string `env:"KUBERNETES_KUBECONFIG" long:"kubeconfig" description:"kubernetes kubeconfig path" yaml:"kubeconfig"`
	Namespace  string `env:"KUBERNETES_NAMESPACE" long:"namespace" description:"kubernetes namespace where vault pod can be found" yaml:"namespace"`
}

func checkKubernetesConfig(conf KubernetesConfig) error {
	if conf.Kubeconfig == "" {
		return errors.New("kubeconfig is empty")
	}

	if conf.Namespace == "" {
		return errors.New("namespace is empty")
	}

	return nil
}

func createKubeClient(cfg KubernetesConfig) error {
	kubeConfig, err := clientcmd.BuildConfigFromFlags("", cfg.Kubeconfig)
	if err != nil {
		return err
	}

	kubeClient, err = kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		return err
	}

	return nil
}

func workerKubernetes(ctx context.Context, wg *sync.WaitGroup, kubeClient *kubernetes.Clientset, namespace string, podAddr string) {
	defer wg.Done()

	var errCount int
	var errDelay time.Duration

	for {
		errDelay = (30 * time.Second) * time.Duration(errCount)
		if errDelay > conf.MaxCheckInterval {
			errDelay = conf.MaxCheckInterval
		}

		if errCount > 0 {
			logger.WithFields(log.Fields{
				"delay":   errDelay,
				"podAddr": podAddr,
			}).Info("delaying checks due to errors")
		}

		select {
		case <-ctx.Done():
			logger.WithField("podAddr", podAddr).Info("closing worker")
			return
		case <-time.After(conf.CheckInterval + errDelay):
			logger.WithField("podAddr", podAddr).Info("running checks")

			response, err := kubeClient.CoreV1().RESTClient().Get().Namespace(namespace).Resource("pods").Name(podAddr).SubResource("proxy").Suffix("v1/sys/health").DoRaw(context.Background())

			if err != nil {
				// if http code is not between (interval included) 200 and 206, the library throw an error
				// has vault use custom http code to return seal status, we need to handle it
				// https://developer.hashicorp.com/vault/api-docs/system/health
				if _, isStatus := err.(*kerrors.StatusError); !isStatus {
					errCount++
					nerr := fmt.Errorf("checking seal status: %w", err)

					if err, ok := err.(net.Error); ok && err.Timeout() { //nolint:errorlint
						// It's a network timeout. If it's the first network timeout,
						// don't notify, just try again. This should help with network
						// blips.

						if errCount == 1 {
							logger.WithField("podAddr", podAddr).WithError(err).Error("checking seal status")
							continue
						}
					}

					notify(nerr)
					continue
				}
			}

			var status api.SealStatusResponse
			err = json.Unmarshal(response, &status)
			if err != nil {
				errCount++
				nerr := fmt.Errorf("checking seal status: %w", err)
				notify(nerr)
				continue
			}

			if status.Sealed {
				logger.WithFields(log.Fields{"addr": podAddr, "status": "sealed"}).Info("seal status")
			} else {
				// Not sealed, don't do anything.
				logger.WithFields(log.Fields{"addr": podAddr, "status": "unsealed"}).Info("seal status")
				errCount = 0
				continue
			}

			// Attempt to loop through the tokens and send the unseal request.
			// Each vault-unseal service should have 2 keys. The API allows
			// unsealing from multiple locations, so as long as two nodes are
			// online, the unseal can occur using two keys from one instance, and
			// one key from another
			for i, token := range conf.Tokens {
				logger.WithFields(log.Fields{
					"podAddr":  podAddr,
					"token":    i + 1,
					"progress": status.Progress,
					"total":    status.T,
				}).Info("using unseal token")

				response, err := kubeClient.CoreV1().RESTClient().Post().Namespace(namespace).Resource("pods").Name(podAddr).SubResource("proxy").Suffix("v1/sys/unseal").Body([]byte(fmt.Sprintf(`{"key": "%s"}`, token))).DoRaw(context.Background())

				if err != nil {
					// if http code is not between (interval included) 200 and 206, the library throw an error
					// has vault use custom http code to return seal status, we need to handle it
					// https://developer.hashicorp.com/vault/api-docs/system/health
					if _, isStatus := err.(*kerrors.StatusError); !isStatus {
						notify(fmt.Errorf("using unseal key %d on %v: %w", i+1, podAddr, err))
						errCount++
						continue
					}
				}

				var status api.SealStatusResponse
				err = json.Unmarshal(response, &status)

				if err != nil {
					notify(fmt.Errorf("using unseal key %d on %v: %w", i+1, podAddr, err))
					errCount++
					continue
				}

				logger.WithField("podAddr", podAddr).Info("token successfully sent")
				if !status.Sealed {
					notify(fmt.Errorf("(was sealed) %v now unsealed with tokens", podAddr))
					continue
				}
			}
		}
	}
}
