// Copyright (c) Liam Stanley <liam@liam.sh>. All rights reserved. Use of
// this source code is governed by the MIT license that can be found in
// the LICENSE file.

package main

import (
	"fmt"
	"log/slog"

	"github.com/nicholas-fedor/shoutrrr/pkg/types"
)

var _ types.StdLogger = (*shoutrrrLogger)(nil)

// shoutrrrLogger is an adapter for [log/slog.Logger] to be used with shoutrrr.
type shoutrrrLogger struct {
	logger *slog.Logger
}

func (l *shoutrrrLogger) Print(v ...any) {
	l.logger.With("component", "shoutrrr").Debug(fmt.Sprint(v...)) //nolint:sloglint
}

func (l *shoutrrrLogger) Printf(format string, v ...any) {
	l.logger.With("component", "shoutrrr").Debug(fmt.Sprintf(format, v...)) //nolint:sloglint
}

func (l *shoutrrrLogger) Println(v ...any) {
	l.logger.With("component", "shoutrrr").Debug(fmt.Sprint(v...)) //nolint:sloglint
}
