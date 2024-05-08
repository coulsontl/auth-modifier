package auth_modifier

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

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
}

func (AuthModifier) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.auth_modifier",
		New: func() caddy.Module { return new(AuthModifier) },
	}
}

// UnmarshalCaddyfile 实现caddyfile.Unmarshaler接口
func (a *AuthModifier) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	// 在这里处理Caddyfile中的配置，由于此插件不需要配置参数，所以留空
	return nil
}

func (a *AuthModifier) Provision(ctx caddy.Context) error {
	a.ctx, a.cancel = context.WithCancel(ctx.Context)
	a.logger = ctx.Logger(a)
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
	index := a.Indexes[r.URL.String()]
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
			a.updateIndex(r.URL.String(), len(tokens))
		}
	} else if len(authHeader) > 0 {
		tokens := strings.Split(authHeader, ",")
		if len(tokens) > 0 {
			selectedToken := tokens[index%len(tokens)]
			r.Header.Set("Authorization", selectedToken)
			a.updateIndex(r.URL.String(), len(tokens))
		}
	}

	if len(apiKeyHeader) > 0 {
		apiKeys := strings.Split(apiKeyHeader, ",")
		if len(apiKeys) > 0 {
			selectedApiKey := apiKeys[index%len(apiKeys)]
			r.Header.Set("X-Goog-Api-Key", selectedApiKey)
			a.updateIndex(r.URL.String(), len(apiKeys))
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

	if err := ioutil.WriteFile("indexes.json", data, 0644); err != nil {
		a.logger.Error("Error writing indexes to file", zap.Error(err))
		a.Mutex.Lock()
		a.Changed = true
		a.Mutex.Unlock()
	}
}

func (a *AuthModifier) loadIndexes() {
	data, err := ioutil.ReadFile("indexes.json")
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
