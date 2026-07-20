# NPS

[README](https://github.com/zhangsean/nps/blob/master/README.md)|[中文文档](https://github.com/zhangsean/nps/blob/master/README_zh.md)

# 说明
由于nps已经有二年多的时间没有更新了，存留了不少bug和未完善的功能。

此版本基于 nps 0.26.10的基础上二次开发而来。

***DockerHub***： [NPS](https://hub.docker.com/r/zhangsean/nps) [NPC](https://hub.docker.com/r/zhangsean/npc)

# 交流群
聊天灌水QQ群：770569342,619833483(已满)

# 公益云内网穿透
https://natnps.com/
公益NPS内网穿透服务，长期免费，6M带宽，3条隧道，不限流量，欢迎来嫖，自行注册账号。

# 特价云服务器
国内BGP，游戏开服，虚拟主机 8元/月起、云服务器 15元/月起，游戏云 25元/月起，[专属连接，首月5折](https://www.rainyun.com/MTE4MTAxNA==_)

# 捐赠
![image](image/tip_wx.png)
![image](image/tip_zfb.png)


## 更新日志
- 2026-07-21  v0.27.21
  - **修复**：HTTP 代理上游断连时按阶段记录错误，避免异常日志缺少 `phase`。
  - **优化**：上游请求写入断开时自动重试；读取响应头断开默认仅重试 `GET`、`HEAD`、`OPTIONS`，域名转发可单独开启非幂等请求重试。

- 2026-07-17  v0.27.20
  - **优化**：默认连接参数调整为 `client_connect_timeout_seconds=2`、`target_connect_timeout_seconds=2`、`target_connect_retry_count=2`，缩短单次等待并增加目标连接重试机会。
  - **新增**：`nps.conf` 增加 `upstream_response_timeout_seconds`，用于限制 nps HTTP 代理等待上游响应头的时间，默认 0 不限制；超时返回 504。
  - **优化**：HTTP/HTTPS 访问日志在异常、错误响应和目标连接重试事件中增加 `phase`、`slowest_phase`、`slowest_phase_ms`、`target_connect_ms`、`request_write_ms`、`response_header_ms`、`response_write_ms`，普通成功请求保持轻量字段。
  - **新增**：文件访问模式支持类似 nginx autoindex 的目录浏览，文件目录默认 `/files`；选择代理到服务端本地时客户端 ID 自动固定为 `1`，并由 nps 直接提供本地目录访问。
  - **新增**：文件访问模式支持浏览认证和管理功能，配置 `allow_browse=true` 和 `browse_password` 后，目录列表和直接文件下载都必须先登录查看；配置 `allow_upload=true` 和 `upload_password` 后，输入管理密码可浏览、创建文件夹、上传和删除文件。
  - **优化**：文件访问列表将目标列显示为本地目标路径，并在端口后新增访问前缀列，便于直接确认目录访问配置。
  - **修复**：文件访问本地路径支持 Windows 下 `/d/tmp` 这类 Git Bash/MSYS 风格路径，保存后保持原始写法，并在运行时正确映射到 `D:\tmp`；兼容已保存成 `\d\tmp` 的历史配置。
  - **优化**：Web 页面版权年份自动显示当前年份，避免每年手动维护。
  - **优化**：登录页用户名或密码错误、验证码错误提示改为 i18n 文案。
  - **优化**：登录页密码框按 Tab 直接跳转到验证码输入框，登录失败后自动刷新验证码并清空验证码输入。
  - **修复**：隧道端口选择器首次打开时默认使用表单当前端口所在百位端口段作为起始端口，例如当前端口 `6188` 时从 `6100` 加载。
  - **修复**：新增或编辑域名、隧道保存后返回列表时保留当前搜索框内容，避免列表被重置为空搜索。

- 2026-07-16  v0.27.19
  - **优化**：当前域名或隧道配置多个 target 时，目标连接重试会在当前 target 列表内继续轮询尝试，避免单个 target 不可达时反复重试同一地址。
  - **优化**：nps LocalProxy 本地代理目标连接重试同样支持按当前 target 列表轮询，保持与 npc 目标连接重试策略一致。
  - **修复**：修正 SOCKS5 UDP 日志中端口输出方式，避免将端口号按单个字符输出。

- 2026-07-15  v0.27.18
  - **新增**：`nps.conf` 增加 `target_connect_retry_interval_ms`，支持目标连接失败后、下一次重试前随机等待，默认 0 不等待；nps LocalProxy 触发目标连接重试时会写入 `event=target_connect_retry` 的 access.log 事件。
  - **修复**：HTTP 代理连接目标后端失败时返回 502，等待后端响应超时时返回 504，避免将上游错误误报为 404。

- 2026-07-14  v0.27.17
  - **修复**：nps LocalProxy 本地代理连接目标服务器时复用 `target_connect_timeout_seconds` 和 `target_connect_retry_count`，避免本地直连目标不可达时被系统 TCP connect 默认超时拖到百秒级。
  - **新增**：HTTP/HTTPS 访问日志增加 `http_access_log_exclude_errors` 和 `http_access_log_exclude_error_types`，支持按错误文本或错误类型过滤无效 CONNECT 扫描等噪声日志。
  - **新增**：`nps.conf` 增加 `client_connect_timeout_seconds`，可配置 nps 发起代理转发时等待 npc 接受新转发连接的秒数，默认 5 秒，便于后端不可达或客户端异常时快速失败。
  - **新增**：`nps.conf` 增加 `target_connect_timeout_seconds` 和 `target_connect_retry_count`，支持 npc 连接目标服务器失败或超时时按目标连接超时和重试次数配置重试并记录重试日志，默认重试 1 次，配置 0 可关闭。
  - **修复**：修复 HTTP/HTTPS 代理 Host 未匹配或连接目标后端失败并返回 404 时未写入访问日志的问题。

- 2026-07-13  v0.27.16
  - **新增**：`npc` 支持通过 `-cip_url`、环境变量 `NPC_CIP_URL` 或 `npc.conf` 的 `cip_url` 定时获取公网 IP，并在 IP 变化时自动上报到服务端；默认接口为 `http://www.3322.org/dyndns/getip`，默认检测频率为 1 小时，响应内容会用正则提取 IPv4 地址以兼容非纯 IP 文本。
  - **新增**：`nps.conf` 增加 `http_access_log_path`，HTTP/HTTPS 代理请求会以 JSON Lines 写入独立访问日志文件，记录时间戳、请求方法、URL、响应状态码、请求/响应字节数和处理耗时，并支持异步写入、按大小轮转压缩、路径排除、慢请求阈值、query 脱敏和字段白名单，便于 Promtail/Loki 采集。
  - **新增**：HTTP/HTTPS 访问日志增加 `http_access_log_exclude_hosts`，支持按 Host 过滤日志，支持 `*.example.com` 这类通配，未配置端口时可匹配带端口的请求 Host。
  - **修复**：修复 nps Docker 镜像缺少时区数据库导致无法识别 `Asia/Shanghai`、日志时间不符合当前时区的问题。

- 2026-07-07  v0.27.15
  - **新增**：客户端列表支持手动刷新公网 IP 归属地，刷新结果会写入持久化缓存，便于修正第三方接口返回的错误地域。
  - **优化**：HTTP/HTTPS 空 Host/SNI 解析失败日志降级为 Trace，减少扫描和探测流量带来的后台日志噪声。
  - **优化**：管理后台统一使用 3 秒自动消失的提示框和站内确认弹窗，替换浏览器原生 `alert` / `confirm`。
  - **优化**：客户端、隧道、域名等列表页操作成功后只刷新当前表格，并保留当前分页，避免跳转到后续页时被刷新回第 1 页。

- 2026-07-07  v0.27.14
  - **新增**：`npc` 支持通过 `-cip`、环境变量 `NPC_CIP` 或 `npc.conf` 的 `cip` 手动上报客户端展示 IP，便于 Docker/gost 转发场景在服务端客户端列表显示公网 IP。
  - **新增**：客户端列表支持在公网 IP 后显示归属地，并增加内存缓存、失败短缓存和第三方接口限流暂停，避免频繁调用 IP 归属地接口。
  - **优化**：客户端归属地结果持久化到 `clients.json` 的 `ClientRegion`、`ClientIp` 字段，重启后优先复用已保存结果，并将默认归属地查询切换为 `ip.cn`，展示精简为省市。

- 2026-07-03  v0.27.13
  - **修复**：修复客户端列表、隧道列表、域名列表中模板 JS 拼接导致的浏览器语法错误，避免列表页无法正常渲染。
  - **优化**：统一内联 JS 中 `web_base_url`、`bridge_type` 等模板变量的字符串渲染方式，降低后续模板解析风险。
  - **优化**：减少 HTTPS 连接相关的噪声日志。

- 2026-06-30  v0.27.12
  - **修复**：修复 nps mux 接收队列可能空转导致资源占用异常的问题。
  - **优化**：优化镜像体积与构建流程，增加前端资源压缩和二进制压缩。
  - **优化**：更新默认配置项和默认值。
  - **CI**：修复发布流程中的 Go 模板压缩处理、废弃 `go get` 用法和 Docker 镜像预构建流程。

- 2026-05-27  v0.27.11
  - **优化**：改进端口选择逻辑，返回更合理的可用端口结果。
  - **优化**：优化列表展示与搜索功能，并补充时间单位翻译。

- 2026-05-07  v0.27.9
  - **新增**：添加端口选择器功能，包含前端界面和后端逻辑。
  - **CI**：升级 GitHub Actions 相关版本，优化构建和发布流程。
  - **CI**：修复 Dockerfile 中 `entrypoint.sh` 可执行权限问题。
  - **CI**：优化 spk 构建流程，添加本地分发包生成和处理逻辑。
  - **CI**：重构 GitHub Release 创建流程，并改进 GitHub CLI 安装方式。

- 2024-10-27  v0.26.21
  - **特性**：Http和TCP代理支持克隆功能，方便快捷复制代理
  - **特性**：相同客户端的Http和TCP代理列表便捷切换
  - **特性**：NPS默认启动一个本地代理客户端，不需要额外启动客户端，即可象 Nginx 一样配置Http和TCP代理，这个本地代理模式有配置界面还实时生效比 Nginx 更方便

- 2024-06-01  v0.26.19
  - golang 版本升级到 1.22.
  - 增加自动https，自动将http 重定向（301）到 https.
  - 客户端命令行方式启动支持多个隧道ID，使用逗号拼接，示例：`npc -server=xxx:8024 -vkey=ytkpyr0er676m0r7,iwnbjfbvygvzyzzt` .
  - 移除 nps.conf 参数 `https_just_proxy` , 调整 https 处理逻辑，如果上传了 https 证书，则由nps负责SSL (此方式可以获取真实IP)，
      否则走端口转发模式（使用本地证书,nps 获取不到真实IP）， 如下图所示。
    ![image](image/new/https.png)



- 2024-02-27  v0.26.18
  ***新增***：nps.conf 新增 `tls_bridge_port=8025` 参数，当 `tls_enable=true` 时，nps 会监听8025端口，作为 tls 的连接端口。
             客户端可以选择连接 tls 端口或者非 tls 端口： `npc.exe  -server=xxx:8024 -vkey=xxx` 或 `npc.exe  -server=xxx:8025 -vkey=xxx -tls_enable=true`


- 2024-01-31  v0.26.17
  ***说明***：考虑到 npc 历史版本客户端众多，版本号不同旧版本客户端无法连接，为了兼容，仓库版本号将继续沿用 0.26.xx


- 2024-01-02  v0.27.01  (已作废，功能移动到v0.26.17 版本)
  ***新增***：tls 流量加密，(客户端忽略证书校验，谨慎使用，客户端与服务端需要同时开启，或同时关闭)，使用方式：
             服务端：nps.conf `tls_enable=true`;
             客户端：npc.conf `tls_enable=true` 或者 `npc.exe  -server=xxx -vkey=xxx -tls_enable=true`


- 2023-06-01  v0.26.16
  ***修复***：https 流量不统计 Bug 修复。
  ***新增***：新增全局黑名单IP，用于防止被肉鸡扫描端口或被恶意攻击。
  ***新增***：新增客户端上次在线时间。


- 2023-02-24  v0.26.15
  ***修复***：更新程序 url 更改到当前仓库中
  ***修复***：nps 在外部路径启动时找不到配置文件
  ***新增***：增加 nps 启动参数，`-conf_path=D:\test\nps`,可用于加载指定nps配置文件和web文件目录。
  ***window 使用示例：***
  直接启动：`nps.exe -conf_path=D:\test\nps`
  安装：`nps.exe install -conf_path=D:\test\nps`
  安装启动：`nps.exe start`

  ***linux 使用示例：***
  直接启动：`./nps -conf_path=/app/nps`
  安装：`./nps install -conf_path=/app/nps`
  安装启动：`nps start -conf_path=/app/nps`



- 2022-12-30  v0.26.14
  ***修复***：API 鉴权漏洞修复


- 2022-12-19
***修复***：某些场景下丢包导致服务端意外退出
***优化***：新增隧道时，不指定服务端口时，将自动生成端口号
***优化***：API返回ID, `/client/add/, /index/addhost/，/index/add/ `
***优化***：域名解析、隧道页面，增加[唯一验证密钥]，方便搜查


- 2022-10-30
***新增***：在管理面板中新增客户端时，可以配置多个黑名单IP，用于防止被肉鸡扫描端口或被恶意攻击。
***优化***：0.26.12 版本还原了注册系统功能，使用方式和以前一样。无论是否注册了系统服务，直接执行 nps 时只会读取当前目录下的配置文件。


- 2022-10-27
***新增***：在管理面板登录时开启验证码校验，开启方式：nps.conf `open_captcha=true`，感谢 [@dongFangTuring](https://github.com/dongFangTuring) 提供的PR


- 2022-10-24:
***修复***：HTTP协议支持WebSocket(稳定性待测试)


- 2022-10-21:
***修复***：HTTP协议下实时统计流量，能够精准的限制住流量（上下行对等）
***优化***：删除HTTP隧道时，客户端已用流量不再清空


- 2022-10-19:
***BUG***：在TCP协议下，流量统计有问题，只有当连接断开时才会统计流量。例如，限制客户端流量20m,当传输100m的文件时，也能传输成功。
***修复***：TCP协议下实时统计流量，能够精准的限制住流量（上下行对等）
***优化***：删除TCP隧道时，客户端已用流量不再清空
![image](image/new/tcp_limit.png)


- 2022-09-14:
修改NPS工作目录为当前可执行文件目录（即配置文件和nps可执行文件放在同一目录下，直接执行nps文件即可），去除注册系统服务，启动、停止、升级等命令
