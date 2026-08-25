# Bifrost Enhanced

这是 [Bifrost](https://github.com/maximhq/bifrost) 的增强分支。官方功能与文档请查看[上游项目](https://github.com/maximhq/bifrost)。

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

WebSearch 由支持原生搜索的上游模型执行；本 fork 不再把不支持搜索的模型自动改接 MCP 搜索。内置 DeepSeek provider 要使用其原生搜索，须在对应 key 或 alias 上启用 `use_anthropic_endpoints`。
