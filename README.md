## AuthModifier Caddy 插件

AuthModifier 是一个为 Caddy v2 设计的自定义 HTTP 处理器插件，它允许您对传入的 HTTP 请求进行认证头（如 `Authorization`）的修改和管理，以及对特定 API 密钥进行轮换和选择。此插件特别适用于需要对请求进行动态认证处理的场景。

### 功能特点

- **动态认证头修改**：允许根据请求的 URL 动态修改 `Authorization` 头。
- **API 密钥轮换**：支持对 `X-Goog-Api-Key` 等 API 密钥进行轮换，实现负载均衡和密钥管理。
- **索引文件管理**：通过索引文件跟踪和管理不同 URL 的认证状态，支持动态更新。
- **灵活配置**：支持在 Caddyfile 中配置索引文件的路径，实现灵活部署。

### 安装

由于 AuthModifier 是一个自定义插件，您需要从源码编译 Caddy 并包含该插件。您可以使用 `xcaddy` 工具来简化这个过程：

```bash
xcaddy build --with github.com/coulsontl/auth-modifier
```

### Caddyfile 配置

在 Caddyfile 中使用 AuthModifier 插件，您可以按照以下方式配置：

```caddyfile
(reverse_proxy_headers) {
    header_up User-Agent "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
}

{
    order auth_modifier before reverse_proxy
}

:3001 {
    auth_modifier "auth/index_3001.json"
    reverse_proxy "https://generativelanguage.googleapis.com" {
        import reverse_proxy_headers
        header_up Host "generativelanguage.googleapis.com"
    }
}

:3002 {
    auth_modifier "auth/index_3002.json"
    reverse_proxy "http://api.openai.com" {
        import reverse_proxy_headers
        header_up Host "api.openai.com"
    }
}
```
其中 auth/index_xx.json 是索引文件的相对路径，该文件用于存储 URL 与认证信息的索引映射。

### 使用示例
假设您有多个 API 密钥，需要根据不同的请求轮换使用，您可以在请求的 X-Goog-Api-Key 或 Authorization 插件会根据索引文件中记录的索引，选择合适的密钥进行请求。
```sh
# 示例1
curl --request GET \
  --url http://127.0.0.1:3001/v1/models \
  --header 'x-goog-api-key: key1,key2,key3'

# 示例2
curl --location 'http://127.0.0.1:3002/v1/chat/completions' \
  --header 'Content-Type: application/json' \
  --header 'Authorization: Bearer key1,key2,key3' \
  --data '{
    "model": "gpt-3.5-turbo",
    "messages": [{"role": "user", "content": "鲁迅为什么暴打周树人"}]
  }'
```

### 注意事项
* 确保索引文件的路径对 Caddy 进程是可访问和可写的。
* 如果在 Caddyfile 中配置了多个实例使用相同的索引文件，请确保实现了适当的并发控制机制，以避免数据冲突。