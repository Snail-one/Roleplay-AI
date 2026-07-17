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

	"roleloom/internal/config"
	"roleloom/internal/httpapi"
	"roleloom/internal/security"
	"roleloom/internal/store"
)

func main() {
	configPath := flag.String("config", "config.json", "配置文件路径")
	address := flag.String("address", "", "覆盖监听地址")
	staticDir := flag.String("static", "", "覆盖前端静态目录")
	flag.Parse()
	if err := run(*configPath, *address, *staticDir); err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
		os.Exit(1)
	}
}
func run(configPath, addressOverride, staticOverride string) error {
	cfg, created, err := config.LoadOrCreate(configPath)
	if err != nil {
		return err
	}
	if created {
		log.Printf("已生成配置文件 %s", configPath)
	}
	password := os.Getenv("ROLELOOM_ADMIN_PASSWORD")
	if len([]rune(password)) < 12 {
		return errors.New("ROLELOOM_ADMIN_PASSWORD 必须至少包含 12 个字符")
	}
	masterKey, err := security.ParseMasterKey(os.Getenv("ROLELOOM_MASTER_KEY"))
	if err != nil {
		return err
	}
	st, err := store.Open(cfg.Server.DatabasePath)
	if err != nil {
		return fmt.Errorf("打开数据库: %w", err)
	}
	defer st.Close()
	changed, err := st.SyncAdminPassword(context.Background(), password)
	if err != nil {
		return fmt.Errorf("初始化管理员密码: %w", err)
	}
	if changed {
		log.Printf("管理员密码已初始化或更新，已有登录会话已撤销")
	}
	profiles, err := st.ListModelProfiles(context.Background())
	if err != nil {
		return err
	}
	for _, p := range profiles {
		if p.HasAPIKey {
			if _, err := security.Decrypt(masterKey, p.APIKeyEncrypted); err != nil {
				return fmt.Errorf("主密钥无法解密模型档案 %q: %w", p.Name, err)
			}
		}
	}
	api, err := httpapi.New(httpapi.Options{Store: st, MasterKey: masterKey, CookieSecure: cfg.Server.SecureCookie, Logf: log.Printf})
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.Handle("/api/", api.Handler())
	staticDir := strings.TrimSpace(staticOverride)
	if staticDir == "" {
		staticDir = cfg.Server.StaticDir
	}
	spa, staticErr := httpapi.NewSPAHandler(staticDir)
	if staticErr == nil {
		mux.Handle("/", spa)
	} else if errors.Is(staticErr, os.ErrNotExist) {
		mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "前端尚未构建，请在 web 目录运行 npm run build。", 404)
		})
		log.Printf("前端目录 %s 不存在，仅启动 API", staticDir)
	} else {
		return staticErr
	}
	address := strings.TrimSpace(addressOverride)
	if address == "" {
		address = cfg.Server.Address
	}
	server := &http.Server{Addr: address, Handler: mux, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, IdleTimeout: 2 * time.Minute}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errs := make(chan error, 1)
	go func() { log.Printf("RoleLoom 已启动：http://%s", address); errs <- server.ListenAndServe() }()
	select {
	case err := <-errs:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdown)
	}
}
