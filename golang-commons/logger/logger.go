/*
Copyright The Platform Mesh Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package logger

import (
	"context"
	"errors"
	"io"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/go-logr/zerologr"
	"github.com/rs/zerolog"

	"go.platform-mesh.io/golang-commons/context/keys"
)

type Level zerolog.Level

const RequestIdLoggerKey = "rid"

// StdLogger is a global default logger, please use with care and prefer creating your own instance
var StdLogger, _ = New(DefaultConfig())

// Config defines the logger configuration
type Config struct {
	Name   string
	Level  string
	NoJSON bool
	Output io.Writer
}

// SetDefaults set config default values
func (c *Config) SetDefaults() {
	if c.Name == "" {
		_, fileName, _, _ := runtime.Caller(0)
		c.Name = fileName
	}

	if c.Level == "" {
		c.Level = "info"
	}

	if c.Output == nil {
		c.Output = os.Stdout
	}
}

// DefaultConfig returns a logger configuration with defaults set
func DefaultConfig() Config {
	c := Config{}
	c.SetDefaults()

	return c
}

// Logger is a wrapper around a Zerolog logger instance
type Logger struct {
	zerolog.Logger
}

// ComponentLogger returns a new child logger that inherits all settings but adds a new component field
func (l *Logger) ComponentLogger(component string) *Logger {
	return l.ChildLogger("component", component)
}

// SubLogger returns a new child logger that inherits all settings but adds a new string key field
func (l *Logger) ChildLogger(key string, value string) *Logger {
	return NewFromZerolog(l.With().Str(key, value).Logger())
}

var ErrInvalidKeyValPair = errors.New("invalid key value pair")

// SubLogger returns a new child logger that inherits all settings but adds a number of new string key field
func (l *Logger) ChildLoggerWithAttributes(keyVal ...string) (*Logger, error) {
	if len(keyVal)%2 != 0 {
		return nil, ErrInvalidKeyValPair
	}
	var key string
	for i, v := range keyVal {
		if i%2 == 0 {
			key = v
			continue
		}
		l = l.ChildLogger(key, v)
	}
	return l, nil
}

// MustChildLogger returns a new child logger that inherits all settings but adds a number of new string key field. It panics in case of wrong use.
func (l *Logger) MustChildLoggerWithAttributes(keyVal ...string) *Logger {
	logger, err := l.ChildLoggerWithAttributes(keyVal...)
	if err != nil {
		l.Fatal().Err(err).Msg("failed to create child logger")
	}
	return logger
}

// Level wraps the underlying zerolog level func to Platform Mesh logger level
func (l *Logger) Level(lvl Level) *Logger {
	return NewFromZerolog(l.Logger.Level(zerolog.Level(lvl)))
}

// Logr returns a new logger that fulfills the log.Logr interface
func (l *Logger) Logr() logr.Logger {
	return zerologr.New(&l.Logger)
}

// New returns a new Logger instance for a given service name and log level
func New(config Config) (*Logger, error) {
	zerologLevel, err := zerolog.ParseLevel(strings.ToLower(config.Level))
	if err != nil {
		return nil, err
	}

	logDest := config.Output
	if config.NoJSON {
		logDest = zerolog.ConsoleWriter{Out: config.Output, TimeFormat: time.RFC3339}
	}

	logger := &Logger{
		Logger: zerolog.New(logDest).Level(zerologLevel).With().Timestamp().Caller().Str("service", config.Name).Logger(),
	}

	return logger, nil
}

// NewFromZerolog returns a new Logger from a Zerolog instance
func NewFromZerolog(logger zerolog.Logger) *Logger {
	return &Logger{logger}
}

// NewFromZerolog returns a new Logger from a Zerolog instance and adds the Request id to the logger Context
func NewRequestLoggerFromZerolog(ctx context.Context, logger zerolog.Logger) *Logger {
	// Requesting value from ctx directly to avoid cyclic dependency to middleware package
	var requestId string
	if val, ok := ctx.Value(keys.RequestIdCtxKey).(string); ok {
		requestId = val
	}
	logger = logger.With().Str(RequestIdLoggerKey, requestId).Logger()
	return &Logger{logger}
}

func SetLoggerInContext(ctx context.Context, log *Logger) context.Context {
	return context.WithValue(ctx, keys.LoggerCtxKey, log)
}

// LoadFromContext returns the Logger from a given context
func LoadLoggerFromContext(ctx context.Context) *Logger {
	value := ctx.Value(keys.LoggerCtxKey)

	log, ok := value.(*Logger)
	if !ok {
		return StdLogger
	}

	return log
}
