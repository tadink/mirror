package logger

import (
	"github.com/gookit/slog"
	"github.com/gookit/slog/handler"
	"github.com/gookit/slog/rotatefile"
)

var Logger *slog.Logger

func InitLogger() {
	handle := handler.MustRotateFile("logs/mirror.log", rotatefile.EveryDay, func(c *handler.Config) {
		c.BackupNum = 2
		c.Levels = slog.AllLevels
		c.UseJSON = true
	})
	Logger = slog.NewWithHandlers(handle)
}

func Error(args ...any) {
	Logger.Error(args)
}

func Fatal(args ...any) {
	Logger.Fatal(args)
}
func Info(args ...any) {
	Logger.Info(args)
}
