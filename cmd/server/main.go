package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"roleloom/internal/agent"
	"roleloom/internal/ai/provider"
	"roleloom/internal/config"
	"roleloom/internal/httpapi"
)

func main() {
	configPath := flag.String("config", "config.json", "本地 JSON 配置文件路径")
	address := flag.String("address", "", "HTTP 监听地址，覆盖配置文件")
	staticDirectory := flag.String("static", "", "前端静态文件目录，覆盖配置文件")
	flag.Parse()
	if err := run(*configPath, *address, *staticDirectory); err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
		os.Exit(1)
	}
}

func run(configPath, addressOverride, staticOverride string) error {
	appConfig, created, err := config.LoadOrCreate(configPath)
	if err != nil {
		return err
	}
	if created {
		fmt.Printf("已生成默认配置文件：%s\n", configPath)
		fmt.Println("请填写 api 配置后重新启动。")
		return nil
	}

	modelClient, err := provider.New(provider.Config{
		Provider:  appConfig.API.Provider,
		APIURL:    appConfig.API.APIURL,
		APIKey:    appConfig.API.APIKey,
		Model:     appConfig.API.Model,
		MaxTokens: appConfig.API.MaxOutputTokens,
		Timeout:   time.Duration(appConfig.API.TimeoutSeconds) * time.Second,
	})
	if err != nil {
		return fmt.Errorf("创建模型客户端: %w", err)
	}
	systemPrompt := strings.TrimSpace(appConfig.Agent.SystemPrompt)
	if systemPrompt == "" {
		systemPrompt = config.DefaultSystemPrompt
	}
	agentFactory := func() (httpapi.ChatAgent, error) {
		return agent.New(modelClient, []agent.Tool{
			agent.TimeTool{},
			agent.CalculatorTool{},
		}, agent.Options{
			SystemPrompt:  systemPrompt,
			MaxIterations: appConfig.Agent.MaxIterations,
		})
	}
	apiServer, err := httpapi.New(agentFactory, httpapi.Options{
		SessionTTL:     time.Duration(appConfig.Server.SessionTTLMinutes) * time.Minute,
		AllowedOrigins: appConfig.Server.AllowedOrigins,
		Logf:           log.Printf,
	})
	if err != nil {
		return fmt.Errorf("创建 HTTP API: %w", err)
	}

	staticDirectory := strings.TrimSpace(staticOverride)
	if staticDirectory == "" {
		staticDirectory = appConfig.Server.StaticDir
	}
	mux := http.NewServeMux()
	mux.Handle("/api/", apiServer.Handler())
	spaHandler, staticErr := httpapi.NewSPAHandler(staticDirectory)
	if staticErr == nil {
		mux.Handle("/", spaHandler)
		log.Printf("提供前端静态文件：%s", staticDirectory)
	} else if errors.Is(staticErr, os.ErrNotExist) {
		mux.HandleFunc("/", func(response http.ResponseWriter, _ *http.Request) {
			response.Header().Set("Content-Type", "text/plain; charset=utf-8")
			response.WriteHeader(http.StatusNotFound)
			_, _ = response.Write([]byte("前端尚未构建，请在 web 目录运行 npm install && npm run build，或使用 Vite 开发服务器。\n"))
		})
		log.Printf("前端目录 %s 尚未构建，仅启动 API", staticDirectory)
	} else {
		return fmt.Errorf("加载前端静态文件: %w", staticErr)
	}

	address := strings.TrimSpace(addressOverride)
	if address == "" {
		address = appConfig.Server.Address
	}
	httpServer := &http.Server{
		Addr:              address,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       20 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("RoleLoom Web 服务已启动：http://%s（提供商：%s，模型：%s）", address, appConfig.API.Provider, appConfig.API.Model)
		serverErrors <- httpServer.ListenAndServe()
	}()

	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownContext); err != nil {
			return fmt.Errorf("关闭 HTTP 服务: %w", err)
		}
		return nil
	}
}
