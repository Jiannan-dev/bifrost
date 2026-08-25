# Bifrost Enhanced

这是 [Bifrost](https://github.com/maximhq/bifrost) 的增强分支。官方功能与文档请查看[上游项目](https://github.com/maximhq/bifrost)。

## 增强功能

### Claude Code WebSearch → MCP

当模型不支持 Anthropic原生 Web Search时，Bifrost会拦截 Claude Code的辅助 `web_search_*` 请求，并交给指定的 MCP搜索工具执行。

- 兼容 Claude Code原生 `WebSearch`；
- 支持流式 `server_tool_use` / `web_search_tool_result`；
- 搜索请求绕过 Semantic Cache；
- Provider Prompt Cache不受影响。

搜索工具在 [`config.dokploy.json`](config.dokploy.json) 中配置：

```json
"web_search_fallback_tool": "search-web_search"
```

将它改成 Bifrost发现的完整 MCP工具名；对应 MCP client必须允许并自动执行该工具。

## Dokploy部署

创建 **Docker Compose** 服务：

```text
Repository: https://github.com/Jiannan-dev/bifrost
Branch: enhanced
Compose file: docker-compose.dokploy.yml
```

只需设置两个环境变量：

```text
BIFROST_SETUP_TOKEN=<随机的首次管理员初始化密钥>
BIFROST_ENCRYPTION_KEY=<固定的数据加密密钥>
```

绑定域名：

```text
Service: bifrost
Port: 8080
```

部署使用内置 SQLite，不需要配置外部数据库。Compose会把整个数据目录持久化到 Docker Volume：

```text
bifrost-data → /app/data
```

其中包括：

```text
/app/data/config.db
/app/data/logs.db
```

重新部署容器不会丢失配置和日志。不要删除 `bifrost-data` volume，也不要在首次部署后更换 `BIFROST_ENCRYPTION_KEY`。

Compose同时会挂载 `config.dokploy.json`、构建当前 fork源码，并通过 `/health` 检查服务。

## Claude Code

```bash
export ANTHROPIC_BASE_URL="https://<你的域名>/anthropic"
```

然后正常使用 Claude Code的 `WebSearch`。
