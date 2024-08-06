package app

import (
	"context"
	"errors"
	"golang.org/x/net/netutil"
	"net"
	"net/http"
	"os"
	"seo/mirror/backend"
	"seo/mirror/config"
	"seo/mirror/frontend"
	"seo/mirror/logger"
	"time"
)

type Application struct {
	FrontendServer *http.Server
	BackendServer  *http.Server
}

func (app *Application) Start() {
	l, err := net.Listen("tcp", ":"+config.Conf.Port)
	if err != nil {
		logger.Fatal("net listen", err.Error())
		return
	}
	f, err := frontend.NewFrontend()
	if err != nil {
		logger.Fatal("new frontend", err.Error())
		return
	}
	l = netutil.LimitListener(l, 256*2048)
	app.FrontendServer = &http.Server{Handler: f}
	b, err := backend.NewBackend(f)
	if err != nil {
		logger.Fatal("new backend", err.Error())
		return
	}
	app.BackendServer = &http.Server{Handler: b, Addr: ":" + config.Conf.AdminPort}
	go func() {
		if err := app.FrontendServer.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatal("监听错误" + err.Error())
			os.Exit(1)
		}
	}()
	go func() {
		if err := app.BackendServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatal("监听错误" + err.Error())
			os.Exit(1)
		}
	}()

}
func (app *Application) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	err := app.FrontendServer.Shutdown(ctx)
	if err != nil {
		logger.Error("shutdown error" + err.Error())
	}
	err = app.BackendServer.Shutdown(ctx)
	if err != nil {
		logger.Error("shutdown error" + err.Error())
	}
	defer cancel()
}
