package auth_modifier

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"fmt"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"go.uber.org/zap"
)

func init() {
	caddy.RegisterModule(AuthModifier{})
	httpcaddyfile.RegisterHandlerDirective("auth_modifier", parseCaddyfile)
}

type AuthModifier struct {
	Indexes    map[string]int `json:"indexes"`
	Mutex      sync.RWMutex
	SaveTicker *time.Ticker
	Changed    bool // 追踪索引数据是否有变化
	ctx        context.Context
	cancel     context.CancelFunc
	logger     *zap.Logger
	IndexPath  string // 存储索引文件的路径
}

func (AuthModifier) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.auth_modifier",
		New: func() caddy.Module { return new(AuthModifier) },
	}
}

// ensureDir 确保给定路径的目录存在
func ensureDir(path string) error {
    // 获取路径中的目录部分
    dir := filepath.Dir(path)
    
    // MkdirAll会创建目录，如果目录已经存在，不会返回错误
    if err := os.MkdirAll(dir, 0755); err != nil {
        return err
    }
    return nil
}

// UnmarshalCaddyfile 实现caddyfile.Unmarshaler接口
func (a *AuthModifier) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
    for d.Next() {
        if !d.Args(&a.IndexPath) {
            return d.ArgErr()
        }
		fmt.Println("get params IndexPath:", a.IndexPath)
    }
    return nil
}

func (a *AuthModifier) Provision(ctx caddy.Context) error {
	a.ctx, a.cancel = context.WithCancel(ctx.Context)
	a.logger = ctx.Logger(a)
	// 检查IndexPath是否已设置，如果没有设置，则使用默认路径
    if len(a.IndexPath) == 0 {
        a.IndexPath = "indexes.json" // 默认文件路径
    }
	// 确保文件路径中的目录存在
    if err := ensureDir(a.IndexPath); err != nil {
		a.logger.Error("Error mkdir", zap.Error(err))
    }
	a.loadIndexes()
	// 设置定时任务，每30秒保存一次索引到文件
	a.SaveTicker = time.NewTicker(30 * time.Second)
	go func() {
		for {
			select {
			case <-a.SaveTicker.C:
				a.saveIndexes()
			case <-a.ctx.Done():
				return
			}
		}
	}()
	return nil
}

func (a *AuthModifier) Cleanup() error {
	a.cancel()        // 通知goroutine退出
	a.SaveTicker.Stop() // 停止定时器
	a.saveIndexes()     // 确保在清理时保存一次
	return nil
}

func (a *AuthModifier) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	a.Mutex.RLock()
	index := a.Indexes[r.URL.Path]
	a.Mutex.RUnlock()

	authHeader := r.Header.Get("Authorization")
	apiKeyHeader := r.Header.Get("X-Goog-Api-Key")
	prefix := "bearer "

	if len(authHeader) >= 7 && strings.HasPrefix(strings.ToLower(authHeader[:7]), prefix) {
		token := strings.TrimSpace(authHeader[7:])
		tokens := strings.Split(token, ",")
		if len(tokens) > 0 {
			selectedToken := tokens[index%len(tokens)]
			r.Header.Set("Authorization", "Bearer "+selectedToken)

			a.logger.Debug("Set Authorization", zap.String("Auth-Key", "Bearer "+selectedToken))
			a.updateIndex(r.URL.Path, len(tokens))
		}
	} else if len(authHeader) > 0 {
		tokens := strings.Split(authHeader, ",")
		if len(tokens) > 0 {
			selectedToken := tokens[index%len(tokens)]
			r.Header.Set("Authorization", selectedToken)

			a.logger.Debug("Set Authorization", zap.String("Auth-Key", selectedToken))
			a.updateIndex(r.URL.Path, len(tokens))
		}
	}

	if len(apiKeyHeader) > 0 {
		apiKeys := strings.Split(apiKeyHeader, ",")
		if len(apiKeys) > 0 {
			selectedApiKey := apiKeys[index%len(apiKeys)]
			r.Header.Set("X-Goog-Api-Key", selectedApiKey)

			a.logger.Debug("Set X-Goog-Api-Key", zap.String("Auth-Key", selectedApiKey))
			a.updateIndex(r.URL.Path, len(apiKeys))
		}
	}

	return next.ServeHTTP(w, r)
}

func (a *AuthModifier) updateIndex(url string, length int) {
	a.Mutex.Lock()
	a.Indexes[url] = (a.Indexes[url] + 1) % length
	a.Changed = true
	a.Mutex.Unlock()
}

func (a *AuthModifier) loadIndexes() {
	data, err := os.ReadFile(a.IndexPath)
	if err != nil {
		if !os.IsNotExist(err) {
			a.logger.Error("Error reading indexes file", zap.Error(err))
		}
		a.Indexes = make(map[string]int)
		return
	}
	if err := json.Unmarshal(data, &a.Indexes); err != nil {
		a.logger.Error("Error parsing indexes file", zap.Error(err))
		a.Indexes = make(map[string]int)
	}
}

func (a *AuthModifier) saveIndexes() {
	a.Mutex.Lock()
	if !a.Changed {
		a.Mutex.Unlock()
		return
	}
	data, err := json.Marshal(a.Indexes)
	if err != nil {
		a.logger.Error("Error marshalling indexes", zap.Error(err))
		a.Mutex.Unlock()
		return
	}
	a.Changed = false
	a.Mutex.Unlock()

	if err := os.WriteFile(a.IndexPath, data, 0644); err != nil {
		a.logger.Error("Error writing indexes to file", zap.Error(err))
		a.Mutex.Lock()
		a.Changed = true
		a.Mutex.Unlock()
	}
}

// parseCaddyfile 用于解析Caddyfile并返回中间件处理器
func parseCaddyfile(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
    var m AuthModifier
    err := m.UnmarshalCaddyfile(h.Dispenser)
    if err != nil {
        return nil, err
    }
    // 返回 AuthModifier 的指针
    return &m, nil
}

func main() {
	// 留空，因为Caddy通过插件机制调用此模块
}
