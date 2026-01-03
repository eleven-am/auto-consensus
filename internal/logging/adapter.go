package logging

import (
	"log/slog"
	"strings"
)

type HashiCorpAdapter struct {
	logger *slog.Logger
}

func NewHashiCorpAdapter(logger *slog.Logger) *HashiCorpAdapter {
	return &HashiCorpAdapter{logger: logger}
}

func (a *HashiCorpAdapter) Write(p []byte) (n int, err error) {
	line := strings.TrimSpace(string(p))
	if line == "" {
		return len(p), nil
	}

	level, msg := parseLine(line)

	switch level {
	case slog.LevelError:
		a.logger.Error(msg)
	case slog.LevelWarn:
		a.logger.Warn(msg)
	case slog.LevelInfo:
		a.logger.Info(msg)
	default:
		a.logger.Debug(msg)
	}

	return len(p), nil
}

func parseLine(line string) (slog.Level, string) {
	levelStart := strings.Index(line, "[")
	if levelStart == -1 {
		return slog.LevelDebug, line
	}

	levelEnd := strings.Index(line[levelStart:], "]")
	if levelEnd == -1 {
		return slog.LevelDebug, line
	}
	levelEnd += levelStart

	levelStr := line[levelStart+1 : levelEnd]
	msg := strings.TrimSpace(line[levelEnd+1:])

	var level slog.Level
	switch strings.ToUpper(levelStr) {
	case "ERROR", "ERR":
		level = slog.LevelError
	case "WARN", "WARNING":
		level = slog.LevelWarn
	case "INFO":
		level = slog.LevelInfo
	case "DEBUG", "TRACE":
		level = slog.LevelDebug
	default:
		level = slog.LevelDebug
	}

	return level, msg
}

var _ interface{ Write([]byte) (int, error) } = (*HashiCorpAdapter)(nil)
