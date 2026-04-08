package logger

import (
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const (
	LevelSilent = 0
	LevelInfo   = 1
	LevelDebug  = 2
	LevelTrace  = 3
)

// Setup 初始化基础日志分流器
func Setup(verbosity int, logFile string) {
	zerolog.SetGlobalLevel(zerolog.Disabled)
	if verbosity >= LevelInfo {
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}
	if verbosity >= LevelDebug {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	}
	if verbosity >= LevelTrace {
		zerolog.SetGlobalLevel(zerolog.TraceLevel)
	}

	var logStream io.Writer = io.Discard
	if verbosity > LevelSilent {
		logStream = zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}
	}

	if logFile != "" {
		f, perr := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if perr == nil {
			log.Logger = zerolog.New(zerolog.MultiLevelWriter(logStream, f)).With().Timestamp().Logger()
		} else {
			log.Logger = zerolog.New(logStream).With().Timestamp().Logger()
			log.Error().Msgf("❌ 无法打开日志文件 (%s): %v", logFile, perr)
		}
	} else {
		log.Logger = zerolog.New(logStream).With().Timestamp().Logger()
	}
}

// 门面模式封装，保证极低消耗
func Tracef(format string, v ...interface{}) {
	log.Trace().Msgf(format, v...)
}

func Debugf(format string, v ...interface{}) {
	log.Debug().Msgf(format, v...)
}

func Infof(format string, v ...interface{}) {
	log.Info().Msgf(format, v...)
}

func Errorf(format string, v ...interface{}) {
	log.Error().Msgf(format, v...)
}
