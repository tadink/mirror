package logger

import "log/slog"
import "gopkg.in/natefinch/lumberjack.v2"

func Init() {
	w := &lumberjack.Logger{
		Filename:   "./logs/mirror.log",
		MaxSize:    500, // megabytes
		MaxBackups: 3,
		MaxAge:     28,   //days
		Compress:   true, // disabled by default
	}
	logger := slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)
}
