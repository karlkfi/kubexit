package log

import (
	"os"

	"go.uber.org/zap/zapcore"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var levelMap = map[string]zapcore.Level{
	"debug":  zapcore.DebugLevel,
	"info":   zapcore.InfoLevel,
	"warn":   zapcore.WarnLevel,
	"error":  zapcore.ErrorLevel,
	"dpanic": zapcore.DPanicLevel,
	"panic":  zapcore.PanicLevel,
	"fatal":  zapcore.FatalLevel,
}

var logger = Logger()

func Logger() logr.Logger {
	l := os.Getenv("KUBEXIT_LOG_LEVEL")
	level, ok := levelMap[l]
	if !ok {
		level = zapcore.InfoLevel
	}

	log.SetLogger(zap.New(zap.Level(level)))
	return log.Log.WithName("kubexit")
}

func Error(err error, msg string, keysAndValues ...interface{}) {
	logger.Error(err, msg, keysAndValues...)
}

func Info(msg string, keysAndValues ...interface{}) {
	logger.Info(msg, keysAndValues...)
}

func Warn(msg string, keysAndValues ...interface{}) {
	logger.V(-1).Info(msg, keysAndValues...)
}
