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

	"golang.org/x/term"

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
	st, err := store.Open(cfg.Server.DatabasePath)
	if err != nil {
		return fmt.Errorf("打开数据库: %w", err)
	}
	defer st.Close()
	ctx := context.Background()
	changed, err := ensureAdminPassword(ctx, st, os.Getenv("ROLELOOM_ADMIN_PASSWORD"), promptAdminPassword)
	if err != nil {
		return fmt.Errorf("初始化管理员密码: %w", err)
	}
	if changed {
		log.Printf("管理员密码已初始化或更新，已有登录会话已撤销")
	}
	profiles, err := st.ListModelProfiles(ctx)
	if err != nil {
		return err
	}
	hasEncryptedKeys := false
	for _, p := range profiles {
		if p.HasAPIKey {
			hasEncryptedKeys = true
			break
		}
	}
	masterKey, keyCreated, err := resolveMasterKey(cfg.Server.MasterKeyPath, os.Getenv("ROLELOOM_MASTER_KEY"), hasEncryptedKeys)
	if err != nil {
		return fmt.Errorf("加载主密钥: %w", err)
	}
	if keyCreated {
		log.Printf("已生成主密钥文件 %s；请把它与数据库一起备份", cfg.Server.MasterKeyPath)
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
	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errs := make(chan error, 1)
	go func() { log.Printf("RoleLoom 已启动：http://%s", address); errs <- server.ListenAndServe() }()
	select {
	case err := <-errs:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-signalCtx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(shutdown)
	}
}

func ensureAdminPassword(ctx context.Context, st *store.Store, environmentPassword string, prompt func() (string, error)) (bool, error) {
	initialized, err := st.AdminPasswordInitialized(ctx)
	if err != nil {
		return false, err
	}
	if environmentPassword != "" {
		return st.SyncAdminPassword(ctx, environmentPassword)
	}
	if initialized {
		return false, nil
	}
	if prompt == nil {
		return false, errors.New("管理员密码尚未初始化")
	}
	password, err := prompt()
	if err != nil {
		return false, err
	}
	return st.SyncAdminPassword(ctx, password)
}

func resolveMasterKey(path, environmentKey string, hasEncryptedKeys bool) ([]byte, bool, error) {
	if strings.TrimSpace(environmentKey) != "" {
		key, err := security.ParseMasterKey(environmentKey)
		return key, false, err
	}
	return security.LoadOrCreateMasterKey(path, !hasEncryptedKeys)
}

func promptAdminPassword() (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", errors.New("首次启动需要交互式终端；也可以设置 ROLELOOM_ADMIN_PASSWORD")
	}
	for attempt := 0; attempt < 3; attempt++ {
		fmt.Fprint(os.Stderr, "首次启动，请设置管理员密码（至少 12 个字符）：")
		first, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", fmt.Errorf("读取管理员密码: %w", err)
		}
		if len([]rune(string(first))) < 12 {
			fmt.Fprintln(os.Stderr, "密码太短，请重新输入。")
			continue
		}
		fmt.Fprint(os.Stderr, "再次输入管理员密码：")
		second, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", fmt.Errorf("读取管理员密码确认: %w", err)
		}
		if string(first) != string(second) {
			fmt.Fprintln(os.Stderr, "两次密码不一致，请重新输入。")
			continue
		}
		return string(first), nil
	}
	return "", errors.New("管理员密码初始化失败次数过多")
}
