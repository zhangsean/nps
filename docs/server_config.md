# 服务端配置文件
- /etc/nps/conf/nps.conf

名称 | 含义
---|---
web_port | web管理端口
web_password | web界面管理密码
web_username | web界面管理账号
web_base_url | web管理主路径,用于将web管理置于代理子路径后面
bridge_port  | 服务端客户端通信端口
https_proxy_port | 域名代理https代理监听端口
http_proxy_port | 域名代理http代理监听端口
auth_key|web api密钥
bridge_type|客户端与服务端连接方式kcp或tcp
public_vkey|客户端以配置文件模式启动时的密钥，设置为空表示关闭客户端配置文件连接模式
ip_limit|是否限制ip访问，true或false或忽略
flow_store_interval|服务端流量数据持久化间隔，单位分钟，忽略表示不持久化
log_level|日志输出级别
http_access_log_path|HTTP/HTTPS 代理访问日志文件路径，输出 JSON Lines，包含时间戳、请求方法、URL、响应状态码、请求/响应字节数和处理耗时；异常、错误响应和 LocalProxy 目标连接重试事件会额外输出 `phase`、`slowest_phase` 和阶段耗时字段；为空表示关闭
http_access_log_max_size_mb|HTTP/HTTPS 代理访问日志单文件最大大小，单位 MB，超过后轮转；0 表示关闭内置轮转
http_access_log_max_backups|HTTP/HTTPS 代理访问日志轮转保留文件数，例如 3 会保留 `.1.gz` 到 `.3.gz`
http_access_log_mask_query_keys|访问日志 URL 中需要脱敏的 query key，逗号分隔；默认空表示不脱敏
http_access_log_exclude_paths|访问日志排除路径，逗号分隔，支持 `/static/*` 这类前缀通配
http_access_log_exclude_hosts|访问日志排除 Host，逗号分隔，支持 `*.example.com` 这类通配；未指定端口时会匹配带端口的请求 Host
http_access_log_exclude_errors|访问日志排除错误文本，逗号分隔，支持 `*host could not be parsed*` 这类通配；可用于过滤无效 CONNECT 扫描日志
http_access_log_exclude_error_types|访问日志排除错误类型，逗号分隔，可选值包括 `host_parse_error`、`host_not_matched`、`timeout`、`client_closed`、`upstream_error`、`proxy_error`
http_access_log_min_duration_ms|访问日志最小记录耗时，单位毫秒；0 表示全部记录
http_access_log_fields|访问日志字段白名单，逗号分隔；默认空表示输出全部字段
auth_crypt_key | 获取服务端authKey时的aes加密密钥，16位
p2p_ip| 服务端Ip，使用p2p模式必填
p2p_port|p2p模式开启的udp端口
pprof_ip|debug pprof 服务端ip
pprof_port|debug pprof 端口
disconnect_timeout|客户端连接超时，单位 5s，默认值 60，即 300s = 5mins
client_connect_timeout_seconds|nps 等待 npc 接受新转发连接的超时时间，直接配置秒数，默认 2
target_connect_timeout_seconds|npc 或 nps LocalProxy 连接目标服务器的超时时间，直接配置秒数，默认 2
target_connect_retry_count|npc 或 nps LocalProxy 连接目标服务器失败或超时时的重试次数，默认 2；配置 0 表示不重试
target_connect_retry_interval_ms|npc 或 nps LocalProxy 连接目标服务器失败后、下一次重试前的最大随机等待毫秒数，默认 0 表示不等待；配置 500 表示随机等待 1~500ms
upstream_response_timeout_seconds|nps HTTP 代理等待上游响应头的超时时间，直接配置秒数，默认 0 表示不限制；超时后返回 504，并在访问日志中标记 `phase=response_header`
