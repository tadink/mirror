package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"seo/mirror/app"
	"seo/mirror/config"
	"seo/mirror/db"
	"seo/mirror/frontend"
	"seo/mirror/logger"
	"strconv"
	"syscall"
)

func main() {

	if len(os.Args) < 2 {
		startCmd()
		return
	}
	switch os.Args[1] {
	case "start":
		cmd := exec.Command(os.Args[0])
		err := cmd.Start()
		if err != nil {
			log.Println("start error:", err.Error())
			return
		}
		pid := fmt.Sprintf("%d", cmd.Process.Pid)
		err = os.WriteFile("pid", []byte(pid), os.ModePerm)
		if err != nil {
			fmt.Println("写入pid文件错误", err.Error())
			err = cmd.Process.Kill()
			fmt.Println("关闭进程错误", err.Error())
			return
		}
		fmt.Println("启动成功", pid)
	case "stop":
		data, err := os.ReadFile("pid")
		if err != nil {
			fmt.Println("read pid error", err.Error())
			return
		}

		pid, err := strconv.Atoi(string(data))
		if err != nil {
			fmt.Println("read pid error", err.Error())
			return
		}
		process, err := os.FindProcess(pid)
		if err != nil {
			fmt.Println("find process error", err.Error())
			return
		}
		if runtime.GOOS == "windows" {
			err = process.Signal(syscall.SIGKILL)
		} else {
			err = process.Signal(syscall.SIGTERM)
		}
		if err != nil {
			fmt.Println("process.Signal error", err.Error())
			return
		}
		fmt.Println("镜像程序已关闭")

	}
}

func startCmd() {
	logger.Init()
	err := config.Init()
	if err != nil {
		slog.Error("parse config error:" + err.Error())
		return
	}
	//繁体
	err = frontend.InitS2T()
	if err != nil {
		slog.Error("转繁体功能错误:" + err.Error())
		return
	}
	err = db.InitDB()
	if err != nil {
		slog.Error("数据库错误:" + err.Error())
		return
	}
	application := &app.Application{}

	application.Start()
	ctx, cancelFunc := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancelFunc()
	<-ctx.Done()
	application.Stop()
	slog.Info("exit")

}
