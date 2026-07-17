package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"roleloom/internal/agent"
	"roleloom/internal/ai/provider"
	"roleloom/internal/config"
)

const (
	defaultSystemPrompt = config.DefaultSystemPrompt
	maxInputSize        = 1 << 20
)

func main() {
	configPath := flag.String("config", "config.json", "本地 JSON 配置文件路径")
	flag.Parse()
	if err := run(*configPath); err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
		os.Exit(1)
	}
}

func run(configPath string) error {
	appConfig, created, err := config.LoadOrCreate(configPath)
	if err != nil {
		return err
	}
	if created {
		fmt.Printf("已生成默认配置文件：%s\n", configPath)
		fmt.Println("请填写 api.api_key 后重新启动。")
		return nil
	}

	client, err := provider.New(provider.Config{
		Provider:  appConfig.API.Provider,
		Protocol:  appConfig.API.Protocol,
		BaseURL:   appConfig.API.BaseURL,
		APIKey:    appConfig.API.APIKey,
		Model:     appConfig.API.Model,
		MaxTokens: appConfig.API.MaxOutputTokens,
		Timeout:   time.Duration(appConfig.API.TimeoutSeconds) * time.Second,
	})
	if err != nil {
		return fmt.Errorf("创建模型客户端: %w", err)
	}

	systemPrompt := appConfig.Agent.SystemPrompt
	if strings.TrimSpace(systemPrompt) == "" {
		systemPrompt = defaultSystemPrompt
	}
	chatAgent, err := agent.New(client, []agent.Tool{
		agent.TimeTool{},
		agent.CalculatorTool{},
	}, agent.Options{
		SystemPrompt:  systemPrompt,
		MaxIterations: appConfig.Agent.MaxIterations,
	})
	if err != nil {
		return fmt.Errorf("创建 Agent: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Printf("RoleLoom AI Agent（提供商：%s，模型：%s）\n", appConfig.API.Provider, appConfig.API.Model)
	fmt.Println("命令：/reset 清空上下文，/exit 退出")

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), maxInputSize)
	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			fmt.Println()
			if err := scanner.Err(); err != nil {
				return fmt.Errorf("读取输入: %w", err)
			}
			return nil
		}

		input := strings.TrimSpace(scanner.Text())
		switch input {
		case "":
			continue
		case "/exit", "/quit":
			return nil
		case "/reset":
			chatAgent.Reset()
			fmt.Println("Agent: 上下文已清空。")
			continue
		}

		answer, err := chatAgent.Chat(ctx, input)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			fmt.Fprintln(os.Stderr, "Agent 错误:", err)
			continue
		}
		fmt.Println("Agent:", answer)
	}
}
