package log

import (
	"context"
	"os"

	"github.com/sirupsen/logrus"
)

var (
	// DefaultLogger is the standard logger with global config.
	DefaultLogger *logrus.Logger

	// L is the DefaultLogger with global fields.
	L *logrus.Entry
)

func init() {
	DefaultLogger = logrus.StandardLogger()

	// TODO: make configurable
	// use "severity" instead of "level" for Stackdriver
	DefaultLogger.SetFormatter(
		&logrus.JSONFormatter{
			FieldMap: logrus.FieldMap{
				logrus.FieldKeyLevel: "severity",
			},
		},
	)

	// use stdout instead of stderr for Kubernetes
	DefaultLogger.SetOutput(os.Stdout)

	// TODO: make configurable
	// ignore debug by default
	DefaultLogger.SetLevel(logrus.InfoLevel)

	L = logrus.NewEntry(DefaultLogger)
}

type (
	// loggerKey is globally unique key for storing a logger in a context value
	loggerKey struct{}
)

// RFC3339NanoFixed is time.RFC3339Nano with nanoseconds padded using zeros to
// ensure the formatted time is always the same number of characters.
const RFC3339NanoFixed = "2006-01-02T15:04:05.000000000Z07:00"

// WithLogger returns a new context with the provided logger. Use in
// combination with logger.WithField(s) for great effect.
func WithLogger(ctx context.Context, logger *logrus.Entry) context.Context {
	return context.WithValue(ctx, loggerKey{}, logger)
}

// GetLogger retrieves the current logger from the context. If no logger is
// available, the default logger is returned.
func GetLogger(ctx context.Context) *logrus.Entry {
	logger := ctx.Value(loggerKey{})

	if logger == nil {
		return L
	}

	return logger.(*logrus.Entry)
}

// G is a short alias to GetLogger
var G = GetLogger
