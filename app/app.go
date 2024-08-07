package app

import (
	"context"
	"errors"
	"golang.org/x/net/netutil"
	"log/slog"
	"net"
	"net/http"
	"os"
	"seo/mirror/backend"
	"seo/mirror/config"
	"seo/mirror/frontend"
	"time"
)

type Application struct {
	FrontendServer *http.Server
	BackendServer  *http.Server
}

func (app *Application) Start() {
	l, err := net.Listen("tcp", ":"+config.Conf.Port)
	if err != nil {
		slog.Error("net listen:" + err.Error())
		return
	}
	f, err := frontend.NewFrontend()
	if err != nil {
		slog.Error("new frontend:" + err.Error())
		return
	}
	l = netutil.LimitListener(l, 256*2048)
	app.FrontendServer = &http.Server{Handler: f}
	b, err := backend.NewBackend(f)
	if err != nil {
		slog.Error("new backend" + err.Error())
		return
	}
	app.BackendServer = &http.Server{Handler: b, Addr: ":" + config.Conf.AdminPort}
	go func() {
		if err := app.FrontendServer.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("监听错误" + err.Error())
			os.Exit(1)
		}
	}()
	go func() {
		if err := app.BackendServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("监听错误" + err.Error())
			os.Exit(1)
		}
	}()

}
func (app *Application) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	err := app.FrontendServer.Shutdown(ctx)
	if err != nil {
		slog.Error("shutdown error:" + err.Error())
	}
	err = app.BackendServer.Shutdown(ctx)
	if err != nil {
		slog.Error("shutdown error:" + err.Error())
	}
	defer cancel()
}
