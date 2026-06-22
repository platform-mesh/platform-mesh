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

package testlogger

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"

	"github.com/rs/zerolog"

	"go.platform-mesh.io/golang-commons/logger"
)

type TestLogger struct {
	*logger.Logger
	buffer *bytes.Buffer
}

// New returns a logger with an in memory buffer containing log messages for use in tests
// The logger will write to stdout and the buffer, if you want to hide the log output use HideLogOutput
func New() *TestLogger {
	buf := &bytes.Buffer{}
	cfg := logger.DefaultConfig()
	cfg.Level = "debug"
	cfg.Output = io.MultiWriter(buf, zerolog.ConsoleWriter{Out: os.Stderr})
	l, _ := logger.New(cfg)

	return &TestLogger{
		Logger: l,
		buffer: buf,
	}
}

// HideLogOutput hides the log output from stdout and only logs to the in-memory buffer
func (l *TestLogger) HideLogOutput() *TestLogger {
	l.Logger = logger.NewFromZerolog(l.Output(l.buffer))
	return l
}

type LogMessage struct {
	Message    string                 `json:"message"`
	Level      zerolog.Level          `json:"level"`
	Service    string                 `json:"service"`
	Error      *string                `json:"error"`
	Attributes map[string]interface{} `json:"-"`
}

func (l *TestLogger) GetLogMessages() ([]LogMessage, error) {
	result := make([]LogMessage, 0)
	logString := l.buffer.String()
	messages := strings.Split(logString, "\n")
	for _, message := range messages {
		if message == "" {
			continue
		}
		logMessage := LogMessage{}
		err := json.Unmarshal([]byte(message), &logMessage)
		if err != nil {
			return nil, err
		}

		attributes := map[string]interface{}{}
		err = json.Unmarshal([]byte(message), &attributes)
		if err != nil {
			return nil, err
		}
		logMessage.Attributes = attributes

		result = append(result, logMessage)
	}

	return result, nil
}

func (l *TestLogger) GetMessagesForLevel(level logger.Level) ([]LogMessage, error) {
	return l.getMessagesForLevels(level)
}

// GetErrorMessages returns all log messages with error, fatal and panic level
// If you only want a single of those levels, use GetMessagesForLevel instead
func (l *TestLogger) GetErrorMessages() ([]LogMessage, error) {
	return l.getMessagesForLevels(logger.Level(zerolog.ErrorLevel), logger.Level(zerolog.FatalLevel), logger.Level(zerolog.PanicLevel))
}

func (l *TestLogger) getMessagesForLevels(levels ...logger.Level) ([]LogMessage, error) {
	messages, err := l.GetLogMessages()
	if err != nil {
		return nil, err
	}

	result := []LogMessage{}

	for _, log := range messages {
		shouldContinue := true
		for _, lvl := range levels {
			if logger.Level(log.Level) == lvl {
				shouldContinue = false
			}
		}
		if shouldContinue {
			continue
		}

		result = append(result, log)
	}
	return result, nil
}
