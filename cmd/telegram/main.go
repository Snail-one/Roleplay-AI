package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"roleloom/internal/agent"
	"roleloom/internal/ai/provider"
	"roleloom/internal/config"
	"roleloom/internal/telegram"
)

const defaultTelegramPollTimeout = 30

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
		fmt.Println("请填写 api 配置和 telegram.bot_token 后重新启动。")
		return nil
	}

	botToken := strings.TrimSpace(appConfig.Telegram.BotToken)
	if botToken == "" || botToken == "your-telegram-bot-token" {
		return errors.New("telegram.bot_token 未配置，请填写 BotFather 提供的 Token")
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
	agentFactory := func() (telegram.ChatAgent, error) {
		return agent.New(modelClient, []agent.Tool{
			agent.TimeTool{},
			agent.CalculatorTool{},
		}, agent.Options{
			SystemPrompt:  systemPrompt,
			MaxIterations: appConfig.Agent.MaxIterations,
		})
	}

	pollTimeout := appConfig.Telegram.PollTimeoutSeconds
	if pollTimeout == 0 {
		pollTimeout = defaultTelegramPollTimeout
	}
	telegramClient, err := telegram.NewClient(telegram.ClientConfig{
		BotToken: botToken,
		Timeout:  time.Duration(pollTimeout+15) * time.Second,
	})
	if err != nil {
		return fmt.Errorf("创建 Telegram 客户端: %w", err)
	}
	verificationContext, cancelVerification := context.WithTimeout(context.Background(), 15*time.Second)
	botUser, err := telegramClient.GetMe(verificationContext)
	cancelVerification()
	if err != nil {
		return fmt.Errorf("验证 Telegram Bot Token: %w", err)
	}
	bot, err := telegram.NewBot(telegramClient, agentFactory, telegram.BotOptions{
		AllowedUserIDs:     appConfig.Telegram.AllowedUserIDs,
		PollTimeoutSeconds: pollTimeout,
		Logf:               log.Printf,
	})
	if err != nil {
		return fmt.Errorf("创建 Telegram Bot: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	botName := botUser.FirstName
	if botUser.Username != "" {
		botName = "@" + botUser.Username
	}
	log.Printf("RoleLoom Telegram Bot %s 已启动（提供商：%s，模型：%s）", botName, appConfig.API.Provider, appConfig.API.Model)
	if len(appConfig.Telegram.AllowedUserIDs) == 0 {
		log.Print("警告：telegram.allowed_user_ids 为空，任何能访问机器人的用户都可以调用 AI")
	}
	return bot.Run(ctx)
}
