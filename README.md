# Bifrost Enhanced

这是 [Bifrost](https://github.com/maximhq/bifrost) 的增强分支。官方功能与文档请查看[上游项目](https://github.com/maximhq/bifrost)。

相对上游 `dev`，这个分支只多了下面这些 fork 改动：

1. **特性：Claude Code WebSearch → MCP。** 不支持原生 Web Search 的模型，也能透明执行 Claude Code 的 `WebSearch`。
2. **特性：Dokploy 部署。** 用仓库里的 Compose / Dockerfile / `config.dokploy.json` 直接部署，并附带 CLIProxyAPI（Codex OAuth）sidecar。
3. **修复：Anthropic 模型列表不再裁剪 id。** `/anthropic/v1/models` 与 OpenAI `/v1/models` 一样返回完整的 `provider/model`，避免客户端拿裁过的名字把请求打到错误的提供商。
4. **修复：Responses→Chat 降级时保住前缀缓存。** Claude Code 打自定义 OpenAI 兼容后端时，丢掉 `prompt_cache_key*`、剥掉 billing system 块、把中途的 `role:system` 改写成 `<system-reminder>`，避免打爆 DeepSeek/GLM 的隐式前缀缓存。
5. **修复：Anthropic thinking 回放不再伪造 reasoning item id。** Claude Code 回放未标记的 `thinking` / `redacted_thinking` 时不再铸造 `rs_*`；OpenCode Go（`store:false`）把这种 id 当成服务端句柄会 400。只恢复嵌入的真实 id；这条 400 会剥掉 reasoning id 再重试一次。不改 `prompt_cache_key*`，也不绕过 Semantic Cache。
6. **修复：Anthropic 回放用稳定的 function_call item id。** Anthropic 转 Responses 时不再每轮给 `function_call` 铸造随机 `fc_*`，改为由稳定的 `call_id` 派生。OpenCode DeepSeek 的精确前缀缓存会在第一个变化的 `fc_*` 处断开。只出站剥掉 id 不够：日志仍在重铸，线上缓存仍约 1%。不改 `prompt_cache_key*`，也不绕过 Semantic Cache。

上游同步用 rebase，不引入 merge commit。`enhanced` 上除上述 fork commit 外，历史与上游 `dev` 一致。

## 增强功能

### Claude Code WebSearch → MCP

当模型不支持 Anthropic 原生 Web Search 时，Bifrost 会拦截 Claude Code 的辅助 `web_search_*` 请求，并交给指定的 MCP 搜索工具执行。

- 兼容 Claude Code 原生 `WebSearch`；
- 支持流式 `server_tool_use` / `web_search_tool_result`；
- 搜索请求绕过 Semantic Cache；
- Provider Prompt Cache 不受影响。

搜索工具在 [`config.dokploy.json`](config.dokploy.json) 中配置：

```json
"web_search_fallback_tool": "Exa-web_search_exa"
```

工具全名是 MCP client 的 `Name`（区分大小写）加上 `-` 再加上服务器上的原始工具名。对应 MCP client 必须已启用。

## 修复

### Anthropic `/v1/models` 裁剪模型 id

这是 bug 修复，不是新功能。

OpenAI `/v1/models` 返回完整 id，例如 `CommandCode/deepseek/deepseek-v4-flash`。修复前 Anthropic `/anthropic/v1/models` 会去掉第一个已知 provider 前缀，变成 `deepseek/deepseek-v4-flash`。`deepseek` 恰好是 Bifrost 内置提供商，客户端再用这个名字发请求就会打到错误的提供商。

修复后两条列表接口返回同一个完整 id。Claude Code 等 Anthropic 客户端请使用列表里的完整名字；自定义 OpenAI 兼容提供商若没有 Responses，需要在提供商配置里关闭 Responses、保留 Chat Completions，Bifrost 才会把 `/anthropic/v1/messages` 降级成 Chat Completions。

### Claude Code Responses→Chat 前缀缓存

这也是 bug 修复：Claude Code 走自定义 OpenAI 兼容提供商（CommandCode / DeepSeek / GLM）时，Bifrost 会把 Responses 请求转成 Chat Completions。这些提供商不会过滤 OpenAI 专用字段，于是 Claude Code 的 `prompt_cache_key*`、billing header 的 system 块，以及对话中途的 `role:system` 预算提示会被原样转发，隐式前缀缓存全部 miss。

`ToChatRequest` 现在会：

- 丢掉 `prompt_cache_key` / `prompt_cache_retention` / `prompt_cache_options`（DeepSeek 不支持；严格兼容接口会直接 400）；
- 剥掉以 `x-anthropic-billing-header:` 开头的 system/developer 内容，剥空则整条消息删除；
- 保留开头的 system prompt，把后面的 `role:system` 改写成包在 `<system-reminder>` 里的 user 轮。

原生 OpenAI Chat Completions 不受影响，仍然会带上 `prompt_cache_key`。`CLAUDE_CODE_ATTRIBUTION_HEADER=0` 不能替代这项修复：那个开关只去掉 billing 块，不管中途 system 和 `prompt_cache_key`。

### Anthropic thinking 回放伪造 reasoning item id

这也是 bug 修复：Claude Code 走 `/anthropic/v1/messages` 打官方 OpenCode Go 时，Bifrost 会把未标记的 `thinking` / `redacted_thinking` 转成 Responses reasoning item，并曾经补一个随机 `rs_*` id。OpenCode Go 的 `/v1/responses` 使用 `store: false`，会把这个 id 当成服务端句柄去查，第二轮立刻 400：`Referenced reasoning item ... was not found or has expired`。随机 id 每轮还不一样，隐式前缀缓存也会被打爆。

现在会：

- 只有 `ExtractReasoningItemID` 找回嵌入的真实 OpenAI id 时才带 `id`；否则省略；
- 遇到上述 400 时，只剥掉 reasoning 类型 item 的 id（包括没有 `ResponsesReasoning` 的 thinking-only item），保留摘要、thinking 文本和 `encrypted_content`，再重试一次，不走退避、不占用普通 `MaxRetries`；
- 官方 OpenCode Go / Zen 出站时再剥一层 reasoning id。原生 OpenAI 仍会带上找回的嵌入 id，好让 `encrypted_content` 对得上。

不启用 OpenCode 的 id 嵌入（它自己发的 id 同样不能回放）。不设置 Semantic Cache 绕过。不改 `prompt_cache_key*`。

### Anthropic 回放使用稳定的 function_call item id

这也是 bug 修复：Claude Code 走 `/anthropic/v1/messages` 打官方 OpenCode Go 时，Bifrost 把 Anthropic `tool_use` 转成 Responses `function_call`，并曾经给 **item** `id` 铸造一个每轮都变的随机 `fc_*`。真正用来配对工具结果的是 `call_id`（Anthropic `tool_use` id）。

OpenCode DeepSeek 对整段 input 做精确前缀匹配，缓存会在第一个变化的 `fc_*` 处停住（常见只有大约 1% cache）。muse-spark 会忽略这些 item id，所以同样路径上缓存仍然很高。随机 `fc_*` 不会像 `rs_*` 那样让 OpenCode `store:false` 直接 400，请求能成功，只是几乎不命中缓存。

只在 OpenCode 出站副本上剥掉这些 id **不够**：dashboard / `logs.db` 记的是转换后的内部请求，里面的 `fc_*` 照样每轮新的；那次部署后线上 `cached_read` 仍卡在大约 2k。

现在会：

- 在 Anthropic → Responses 转换时，用 `call_id` 的 sha256 派生稳定的 `fc_*` item id（空 `call_id` 则省略，不再随机铸造）；
- OpenCode Go / Zen 出站仍只剥 reasoning item id（避免 400），**保留** 这个稳定的 function_call item id；
- 原生 OpenAI 拿到同一套派生 id。

不设置 Semantic Cache 绕过。不改 `prompt_cache_key*`。

## Dokploy 部署

创建 **Docker Compose** 服务：

```text
Repository: https://github.com/Jiannan-dev/bifrost
Branch: enhanced
Compose file: docker-compose.dokploy.yml
```

需要的环境变量：

```text
BIFROST_SETUP_TOKEN=<随机的首次管理员初始化密钥>
BIFROST_ENCRYPTION_KEY=<固定的数据加密密钥>
CLIPROXY_API_KEY=<CLIProxyAPI 与 Bifrost 之间的共享密钥>
```

绑定域名：

```text
Service: bifrost
Port: 8080
```

部署使用内置 SQLite，不需要配置外部数据库。Compose 会把整个数据目录持久化到 Docker Volume：

```text
bifrost-data → /app/data
```

其中包括：

```text
/app/data/config.db
/app/data/logs.db
```

重新部署容器不会丢失配置和日志。不要删除 `bifrost-data` volume，也不要在首次部署后更换 `BIFROST_ENCRYPTION_KEY`。

Compose 同时会挂载 `config.dokploy.json`、构建当前 fork 源码，并通过 `/health` 检查服务。CLIProxyAPI 跑在同一 Compose 里，不对外暴露 8317；Bifrost 通过内部网络访问。首次部署后需要在 sidecar 里完成一次 Codex 登录。

## Claude Code

```bash
export ANTHROPIC_BASE_URL="https://<你的域名>/anthropic"
```

然后正常使用 Claude Code 的 `WebSearch`。模型请填 OpenAI 或 Anthropic 列表返回的完整 `provider/model` id。
