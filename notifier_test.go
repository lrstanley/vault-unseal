// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"errors"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestParseEvents(t *testing.T) {
	tests := []struct {
		name    string
		in      []string
		want    []eventKind
		wantErr bool
	}{
		{name: "unset", in: nil, want: nil},
		{name: "empty-entries", in: []string{"", " "}, want: nil},
		{name: "unseal-only", in: []string{"unseal"}, want: []eventKind{eventUnseal}},
		{
			name: "error-group",
			in:   []string{"error"},
			want: []eventKind{eventSealStatusError, eventUnsealError},
		},
		{
			name: "case-and-whitespace",
			in:   []string{" Unseal ", "UNSEAL_ERROR"},
			want: []eventKind{eventUnseal, eventUnsealError},
		},
		{
			name: "group-and-explicit-overlap",
			in:   []string{"error", "unseal_error", "unseal"},
			want: []eventKind{eventUnseal, eventSealStatusError, eventUnsealError},
		},
		{name: "invalid", in: []string{"unseal", "errors"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseEvents(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			gotKinds := slices.Sorted(maps.Keys(got))
			wantKinds := slices.Sorted(slices.Values(tt.want))
			if !slices.Equal(gotKinds, wantKinds) {
				t.Fatalf("got %v, want %v", gotKinds, wantKinds)
			}
		})
	}
}

func TestNotifyConfigAllows(t *testing.T) {
	tests := []struct {
		name   string
		events []string
		allow  []eventKind
		deny   []eventKind
	}{
		{name: "default-allows-all", events: nil, allow: allEventKinds},
		{
			name:   "unseal-only",
			events: []string{"unseal"},
			allow:  []eventKind{eventUnseal},
			deny:   []eventKind{eventSealStatusError, eventUnsealError},
		},
		{
			name:   "errors-only",
			events: []string{"error"},
			allow:  []eventKind{eventSealStatusError, eventUnsealError},
			deny:   []eventKind{eventUnseal},
		},
		{
			name:   "unseal-and-unseal-errors",
			events: []string{"unseal", "unseal_error"},
			allow:  []eventKind{eventUnseal, eventUnsealError},
			deny:   []eventKind{eventSealStatusError},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Config{
				Nodes:           []string{"http://127.0.0.1:8200"},
				Tokens:          []string{"token"},
				AllowSingleNode: true,
				Notify:          NotifyConfig{Events: tt.events},
			}
			if err := c.Validate(t.Context(), slog.New(slog.DiscardHandler)); err != nil {
				t.Fatalf("unexpected validation error: %v", err)
			}

			for _, kind := range tt.allow {
				if !c.Notify.allows(kind) {
					t.Errorf("expected %q to be allowed", kind)
				}
			}
			for _, kind := range tt.deny {
				if c.Notify.allows(kind) {
					t.Errorf("expected %q to be filtered", kind)
				}
			}
		})
	}
}

func TestValidateRejectsInvalidEvents(t *testing.T) {
	c := &Config{
		Nodes:           []string{"http://127.0.0.1:8200"},
		Tokens:          []string{"token"},
		AllowSingleNode: true,
		Notify:          NotifyConfig{Events: []string{"unsealed"}},
	}
	if err := c.Validate(t.Context(), slog.New(slog.DiscardHandler)); err == nil {
		t.Fatal("expected validation error for invalid event")
	}
}

func TestValidateNotifyQueueSettings(t *testing.T) {
	tests := []struct {
		name                       string
		maxElapsed, queueDelay     time.Duration
		legacyElapsed, legacyDelay time.Duration
		wantElapsed, wantDelay     time.Duration
	}{
		{
			name:        "unset-sends-immediately",
			wantElapsed: 0,
			wantDelay:   10 * time.Second,
		},
		{
			name:        "notify-settings",
			maxElapsed:  2 * time.Minute,
			queueDelay:  time.Minute,
			wantElapsed: 2 * time.Minute,
			wantDelay:   time.Minute,
		},
		{
			name:          "legacy-fallback",
			legacyElapsed: 3 * time.Minute,
			legacyDelay:   30 * time.Second,
			wantElapsed:   3 * time.Minute,
			wantDelay:     30 * time.Second,
		},
		{
			name:          "notify-settings-take-precedence",
			maxElapsed:    2 * time.Minute,
			queueDelay:    time.Minute,
			legacyElapsed: 3 * time.Minute,
			legacyDelay:   30 * time.Second,
			wantElapsed:   2 * time.Minute,
			wantDelay:     time.Minute,
		},
		{
			name:        "clamped",
			maxElapsed:  -time.Second,
			queueDelay:  time.Hour,
			wantElapsed: 0,
			wantDelay:   10 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Config{
				Nodes:            []string{"http://127.0.0.1:8200"},
				Tokens:           []string{"token"},
				AllowSingleNode:  true,
				NotifyMaxElapsed: tt.legacyElapsed,
				NotifyQueueDelay: tt.legacyDelay,
				Notify:           NotifyConfig{MaxElapsed: tt.maxElapsed, QueueDelay: tt.queueDelay},
			}
			if err := c.Validate(t.Context(), slog.New(slog.DiscardHandler)); err != nil {
				t.Fatalf("unexpected validation error: %v", err)
			}

			if c.Notify.maxElapsed != tt.wantElapsed {
				t.Errorf("maxElapsed = %v, want %v", c.Notify.maxElapsed, tt.wantElapsed)
			}
			if c.Notify.queueDelay != tt.wantDelay {
				t.Errorf("queueDelay = %v, want %v", c.Notify.queueDelay, tt.wantDelay)
			}
		})
	}
}

func TestNotifyFiltering(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)

	tests := []struct {
		name   string
		events []string
		want   []eventKind
	}{
		{name: "default", events: nil, want: allEventKinds},
		{name: "unseal-only", events: []string{"unseal"}, want: []eventKind{eventUnseal}},
		{name: "errors-only", events: []string{"error"}, want: []eventKind{eventSealStatusError, eventUnsealError}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events, err := parseEvents(tt.events)
			if err != nil {
				t.Fatal(err)
			}

			prev := conf.Load()
			t.Cleanup(func() { conf.Store(prev) })
			conf.Store(&Config{Notify: NotifyConfig{
				URLs:   []string{"logger://"},
				Events: tt.events,
				events: events,
			}})

			for _, kind := range allEventKinds {
				notify(t.Context(), logger, kind, errors.New("test"))
			}

			var got []eventKind
			for len(notifyCh) > 0 {
				got = append(got, (<-notifyCh).kind)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFormatNotification(t *testing.T) {
	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	statusErr := notifyEvent{
		timestamp: ts,
		kind:      eventSealStatusError,
		err:       errors.New("error: checking seal status: context deadline exceeded"),
	}
	unseal := notifyEvent{
		timestamp: ts,
		kind:      eventUnseal,
		err:       errors.New("unseal: (was sealed) http://a:8200 now unsealed with tokens"),
	}
	footer := "\n\nsent from vault-unseal. version: " + version + ", compile date: " + date

	t.Run("errors-only-unchanged", func(t *testing.T) {
		title, body := formatNotification("prod", []notifyEvent{statusErr, statusErr})

		if want := "vault-unseal: prod: 2 error(s) occurred"; title != want {
			t.Errorf("title = %q, want %q", title, want)
		}

		// This must match the format used prior to event filtering being added.
		want := "vault-unseal ran into errors when attempting to check seal status/unseal. here are the errors:\n" +
			"\n2026-01-02T03:04:05Z :: error: checking seal status: context deadline exceeded" +
			"\n2026-01-02T03:04:05Z :: error: checking seal status: context deadline exceeded" +
			footer
		if body != want {
			t.Errorf("body = %q, want %q", body, want)
		}
	})

	t.Run("unseal-only", func(t *testing.T) {
		title, body := formatNotification("prod", []notifyEvent{unseal})

		if want := "vault-unseal: prod: 1 node(s) unsealed"; title != want {
			t.Errorf("title = %q, want %q", title, want)
		}
		if strings.Contains(body, "ran into errors") {
			t.Errorf("unseal-only body should not mention errors: %q", body)
		}
		if !strings.Contains(body, "2026-01-02T03:04:05Z :: unseal: (was sealed) http://a:8200 now unsealed with tokens") {
			t.Errorf("body missing unseal event: %q", body)
		}
	})

	t.Run("mixed", func(t *testing.T) {
		title, _ := formatNotification("prod", []notifyEvent{unseal, statusErr, statusErr})

		if want := "vault-unseal: prod: 1 node(s) unsealed, 2 error(s) occurred"; title != want {
			t.Errorf("title = %q, want %q", title, want)
		}
	})
}
