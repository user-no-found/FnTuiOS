# fnOS 用户运行环境

FnTuiOS 通过飞牛统一网关的 `/app/tuios/` 入口运行。网关把 HTTP 和 WebSocket 请求交给 `${TRIM_APPDEST}/app.sock`，浏览器使用当前飞牛页面的域名。终端连接使用 WebSocket；不需要额外公开 TCP/UDP 端口。桌面入口使用 `iframe`，允许同源飞牛桌面嵌入。

## 身份与进程

`bin/fntuios serve --config FILE` 是包生命周期管理的前台监督进程，须由 root 启动。它只处理本地、继承的匿名控制通道。网络 HTTP 解析在独立的 `gateway_account` 低权限进程中运行。网关请求能够提交给监督进程的字段只有数值 UID 和用户名，没有程序、命令、环境变量、路径或权限标志。

监督进程通过固定的系统 NSS 工具 `getent passwd` 和 `id -G` 交叉检查用户名、UID、主要组和附加组，并使用真实账户的 HOME。UID 小于 1000、65534、应用服务账户，以及不匹配的用户名/UID 均不能创建用户终端。`X-Trim-Isadmin` 不授予 root 权限。每个用户的 worker、独立 `tuios daemon`、`tuios-web` 和 shell 都以该用户的 UID/GID/附加组运行。

`terminal_shell` 默认是 `/bin/bash`。它独立于 NSS 登录 shell：某些 NAS 账户用 `nologin` 限制 SSH，飞牛已认证的用户仍可使用应用终端。FnTuiOS 不更改 OS 登录 shell。终端具有当前用户原有的文件、设备和组权限，不构成额外文件沙箱。

浏览器中的设置面板遵循上游“仅此会话”的行为：修改影响当前 Web 客户端，不写回用户配置或 daemon 的选项。需要持久配置时，编辑本人私有配置或使用 TUIOS 的配置命令；中文显示标签不会改变 `top`、`bottom`、`hidden` 等机器取值。

每次 HTTP 请求、每次 WebSocket 握手都会重新解析账户。用户的组或 HOME 改变时，其旧 worker 先停止并保存，再以新身份数据启动。飞牛负责禁用账户与登录态校验。已建立的 WebSocket 绑定到握手选择的 worker，不从消息中切换 UID。退出飞牛不会立即撤销已建立的连接；`connection_lifetime_seconds` 限制底层连接的最长持续时间，之后重新握手必须重新通过飞牛认证。后台会话保留到应用停止或进程退出。账户 UID 重新分配遵循 Linux 的文件所有权语义，管理员应在重新分配 UID 前处理该 UID 的旧应用数据。

## 粘贴图片

在终端中使用系统粘贴快捷键即可粘贴文本或图片。如果剪贴板同时含有文本和图片，会优先使用文本；粘贴图片时，请先在来源应用中复制图片本身。支持 PNG、JPEG、GIF 和 WebP，单张图片不得超过 8 MiB。

直接粘贴使用浏览器交付给粘贴事件的内容，不主动读取系统剪贴板。浏览器或嵌入页面限制可能导致事件不含图片；此时不会上传图片。上传状态和错误通过短暂提示显示，不占用终端布局空间。

上传开始时会记住原窗格，完成后将图片的本地路径插入该窗格，不会自动按 Enter。上传期间切换窗格不会改变目标；若原窗格已经关闭，应选择另一个窗格后重新粘贴。确认终端中的内容后，再自行按 Enter。只读或已断开的终端不能粘贴图片。

图片保存在当前 NAS 用户的临时目录，默认路径为 `/run/fntuios/<UID>/tuios/paste/`。目录权限为 0700，文件权限为 0600。上游暂存策略为 1 小时过期、最多 50 张且合计不超过 100 MiB；容量清理或 daemon 停止可能更早移除图片。这不是长期存储，也不保证每张图片都保留满 1 小时，需要留存时请及时复制到本人持久目录。

插入的路径属于 NAS 本机。若窗格中正在连接另一台 SSH 主机或运行容器，该主机或容器不一定能访问此路径；请先把图片传到目标环境，或使用已共享的目录。

## 本地信任边界

默认 `trusted_gateway_accounts` 为 `["root"]`，`app.sock` 为 root:root、0600。这是显式部署策略，依赖该设备的统一网关连接者实际使用 root。本机只读配置显示 nginx 的 `/app/` 路由交给 `/var/run/trim_http_cgi.socket`，接收进程 `trim_http_cgi` 使用 UID/GID 0；`trim_open_gateway` 对应其他路由。该路由证据支持默认策略，但不等于已直接观察应用 socket 的真实连接 peer。服务在每个 Unix 连接上检查 `SO_PEERCRED`；请求头本身不证明来源。若设备使用其他系统账户作为连接者，应先核实实际进程身份，再加入该列表。列表必须保留 root，以便包生命周期的本地健康检查工作。非 root 网关账户必须使用同一个主要组，socket 会变为该组的 0660；普通 NAS 账户和应用自身账户不能被列为可信网关。

HTTP 请求须位于配置前缀内。重复身份头、非规范 UID、路径穿越、编码路径、跨站来源和错误 Host 均被拒绝。浏览器的 Origin 必须使用 HTTP/HTTPS，不能携带用户名、路径、查询或片段；WebSocket 和变更请求必须有 Origin。来源策略由 FnPackKit 的 `gatewaypolicy` 运行库执行，应用仅提供已解析的部署配置。

`allow_same_origin_fetch` 默认开启。只有连接已经通过可信 Unix peer 验证、NAS 身份头有效、Host/Origin 语法有效，并且浏览器的 `Sec-Fetch-Site` 明确为 `same-origin` 时，才接受可信系统代理对公网 authority 的改写。该判断依据浏览器生成的请求上下文，不依据页面 JavaScript 的业务判断；`same-site` 和 `cross-site` 均拒绝，不将同主机其他端口当作同源。`X-Forwarded-*` 等客户端转发头不能扩大允许范围。

没有该来源证明时，保持严格策略：完整 Host 与 Origin 相同，或者匹配 `allowed_origins` 中的完整 HTTP/HTTPS Origin。针对 nginx 去掉入口端口的情况，`external_origin_ports` 只补充同主机、配置协议和显式端口的匹配，转发 Host 必须未携带端口。模板保留 `http:5666`、`https:5667`；空对象或省略关闭端口补充。`allowed_origins` 默认空，不支持通配符、用户名、路径、查询或片段。管理员应只配置已核实由可信 NAS 登录入口使用的完整来源。

页面每次建立或重新建立 WebSocket 连接前，都会先向同前缀 `POST /app/tuios/_gateway/grant` 请求短时、一次性连接授权，以覆盖 WebSocket 握手不携带 Fetch Metadata 的浏览器。该请求执行同一来源和身份策略，不启动运行实例；授权绑定 NAS 身份、浏览器外部 Origin 和固定 `/app/tuios/ws` 目标。共享客户端通过独立 WebSocket 子协议项传递授权，不把它放入 URL。网关原子消费后剥离授权项，后端、响应、审计均不接收授权值；重新连接取得新授权。过期、重复使用、身份或来源不匹配的授权会被拒绝。

授权默认有效 30 秒，全局至多 512 张，每用户至多 8 张；同用户达到上限时只淘汰本人的最旧未用授权，全局已满则拒绝签发。配置可调整这些边界。不同端口页面不能读取目标来源的授权响应，也不能把其他 Origin 的授权用于自己的 WebSocket 握手。

`allow_same_origin_fetch=false` 关闭浏览器同源证明适配，仍执行上述严格策略。已有配置缺少该新字段时使用默认 `true`，显式 `false` 不会被覆盖；不改写已有配置文件。手机 WebView 若不发送 Fetch Metadata，也会走严格策略；无法核实的来源会被拒绝并展示连接诊断，不保证所有旧客户端自动兼容。

代理去掉浏览器的认证、NAS Cookie、身份和转发头，在检查外部来源后为内部服务构造一致的 loopback Host/Origin。仅允许收件箱链接的应用 Cookie：浏览器名称为 `fntuios_inbox`，内部名称为 `tuios_inbox`，内容须为唯一的十进制条目 ID，返回路径限定为应用前缀。没有密码或凭据返回浏览器。

匿名控制通道的回复必须对应当前请求的 UID。任何超时、协议错误或传输失败都会永久关闭该通道，停止使用失去同步的连接；迟到的回复不能被下一位用户接收。

每个用户的 Web 服务仅监听独立 loopback 端口，使用随机 256 位内部密码。单独绑定 localhost 不被当作鉴权。密码保存在用户私有运行目录、通过匿名控制通道传给网关，不在命令参数、日志或浏览器响应中出现。其他用户无法访问其 0700 运行目录和 daemon socket。拥有 root 权限的系统进程属于操作系统信任边界。

`allowed_hosts` 非空时进一步限定完整 Host（包括非默认端口），不支持通配符。空列表表示已验证本地网关承担外部域名入口的信任，FnTuiOS 仍检查 Host 语法并执行上述浏览器来源策略。这样可沿用飞牛 LAN 与 FN Connect 的实际访问域名，无需猜测远程域名。可按部署需要填写所有合法入口。

## 可编辑配置

安装时通过以下命令从包模板生成配置；已存在的配置会保留并验证。修改后重启应用生效，无热重载。

```sh
bin/fntuios configure --template etc/gateway.json \
  --output "$TRIM_PKGETC/gateway.json" --app-dir "$TRIM_APPDEST" \
  --state-dir "$TRIM_PKGVAR/fntuios" --run-dir /run/fntuios
bin/fntuios check-config --config "$TRIM_PKGETC/gateway.json"
bin/fntuios prepare-state --config "$TRIM_PKGETC/gateway.json"
bin/fntuios health --config "$TRIM_PKGETC/gateway.json"
bin/fntuios serve --config "$TRIM_PKGETC/gateway.json"
```

| 字段 | 默认值／要求 | 作用 |
| --- | --- | --- |
| `app_dir` | 安装生成的绝对路径 | root 管理的二进制和 `app.sock` 所在目录 |
| `state_dir` | 安装生成的独立持久目录 | 每个 UID 的配置、状态、数据、缓存和日志 |
| `run_dir` | `/run/fntuios` | 用户运行目录、daemon socket、临时内部认证 |
| `prefix` | `/app/tuios` | 与飞牛入口一致，无末尾斜杠；入口自动补斜杠 |
| `gateway_account` | `tuios` | 仅负责 HTTP/WS 代理的低权限系统账户 |
| `trusted_gateway_accounts` | `["root"]` | 必须已核实的网关 Unix peer 账户 |
| `allowed_hosts` | `[]` | 可选的代理转发 Host 白名单，仍独立于浏览器同源证明 |
| `allow_same_origin_fetch` | `true` | 可信 Unix 网关与有效 NAS 身份后的浏览器同源证明适配；显式 `false` 使用严格来源策略 |
| `allowed_origins` | `[]` | 管理员核实的完整 HTTP/HTTPS Origin 例外，不支持通配符 |
| `gateway_grant_lifetime_seconds` | 30，范围 1–300 | 一次性 WebSocket 授权有效期 |
| `gateway_grant_capacity` | 512，范围 1–8192 | 全局未使用连接授权数量上限 |
| `gateway_grant_per_user` | 8，范围 1–全局容量 | 每用户未使用连接授权数量上限 |
| `external_origin_ports` | `{"http":[5666],"https":[5667]}` | 按协议明确列出的 NAS 入口端口；仅用于转发 Host 没有端口的同主机 Origin 匹配，`{}` 关闭 |
| `max_users` | 8，范围 1–128 | 同时保留的用户 worker 上限 |
| `max_connections_per_user` | 4，范围 1–64 | 每个用户的终端 WebSocket 数量上限 |
| `connection_lifetime_seconds` | 3600，范围 60–86400 | 强制重新建立连接的最大间隔 |
| `startup_timeout_seconds` | 30，范围 5–120 | 用户服务启动就绪等待时间 |
| `shutdown_timeout_seconds` | 60，范围 10–300 | 每次等待正常保存完成的观察时间 |
| `state_restore_max_entries` | 100000，范围 1–1000000 | 离线状态归属恢复的目录项扫描预算，仅用于 `prepare-state` |
| `state_restore_max_depth` | 64，范围 1–256 | 离线状态归属恢复的目录深度预算，仅用于 `prepare-state` |
| `user_path` | 系统 bin/sbin 路径 | 终端白名单环境中的 PATH，不继承包安装变量 |
| `language` | `C.UTF-8` | shell 的 LANG/LC_ALL |
| `terminal_shell` | `/bin/bash` | 已安装的绝对 shell 路径 |

根目录、配置和二进制必须由 root 控制，不能让普通账户改写它们或其目录祖先。每个用户目录由本人持有、权限 0700；父目录由 root 持有、权限 0711。监督进程不以 root 打开用户内容。原共享 tuios 配置/会话不会被分配给任何新用户。

安装与升级的回调通过 `libexec/configure --prepare-state` 显式请求状态准备，持有 `service.lock`，生成或验证配置后调用 root 专用 `prepare-state`；普通配置回调只验证或生成配置。准备命令还必须取得 `supervisor.lock`；服务运行时会拒绝恢复。共享 FnPackKit 只处理 `state_dir` 下经 NSS 双向验证的规范 UID 目录，排除网关账户，仅遍历 root 控制的目录，已归属目标用户的目录整树跳过。它保留文件内容和当前权限，通过不跟随链接的目录描述符恢复所有权，跳过符号链接，拒绝硬链接、跨挂载点及特殊节点。不处理 `run_dir`，也不为普通网页请求放宽用户目录检查。预算配置缺失时采用上述默认值，修改后在下一次离线准备时生效。

停止时，先阻止网关访问，再逐用户向 daemon 发送 TERM，等待它完成持久化，最后停止 Web。保存超过观察时间时监督进程继续等待，不用 KILL 截断保存。若包管理器报告停止超时，应查看日志并检查仍在保存的进程。内部进程使用直接子进程句柄和 Linux pidfd，不按未经验证的 PID 文件或进程名称终止其他程序。

## 连接诊断

网页在连接前显示状态；WebSocket 打开但尚无输出时显示等待，收到终端输出后隐藏状态面板。失败时只发起一次有超时的同前缀 `POST /app/tuios/_gateway/status`，显示安全原因和服务端请求 ID；诊断被前置代理拒绝且响应无法解析时仅显示通用提示及 HTTP 状态，不展示原始响应或访问 URL。重试按钮仅在用户点击时请求重新连接，不重启服务。

诊断端点与业务入口执行相同的来源、身份和应用 Cookie 校验，但不启动用户运行实例或终端会话。`ready` 仅说明 HTTP 入口验证通过，不代表 WebSocket 或终端已经正常。连接诊断 JS/CSS 来自 FnPackKit 的 `web/`，应用适配只接入 sip 生命周期事件；共享资产源码摘要独立记录在包内 provenance。

拒绝和代理故障记录固定 `code`、HTTP 状态、服务端生成的 `request_id` 与有限请求形态。审计不记录 Cookie、认证令牌、用户名、域名、路径、查询或终端正文。客户端收到的请求 ID可用于关联服务日志中的同一次失败。前置系统网关自行拒绝的请求可能尚未到达应用，应结合系统入口的状态记录判断。

## 原生包验证

使用普通用户运行 `scripts/verify_latest.py`，参数和工具路径见 [构建验证](build.md)。验证器从实际 FPK 提取二进制，检查包与 ELF 哈希，创建可编辑临时配置并检查非法参数，然后启动实际 worker、daemon 和 Web。所有 HOME、XDG、配置和运行路径位于独立临时目录，测试不建立账户、不接触安装服务。

原生 worker 测试验证当前真实 UID、去除继承环境中的秘密、有序停止及重启后的会话历史。浏览器测试通过受信 Unix peer 的真实 gateway 访问 Web，broker 使用明确的单账户测试响应。Chromium 使用临时 profile 和合成剪贴板数据，检查中文界面、终端输入、图像粘贴、原始 pane 绑定及授权撤销，保存截图、终端文本和 JSON 证据。测试会明确记录单 UID 边界，不把目录分开或模拟身份当作跨 UID 隔离证据。

特权 broker 的降权、附加组、两个真实 UID 文件权限隔离、安装/升级 callback 和实际 FN Connect 登录授权需要另外提供受控设备或专用账户环境。本地非 root 验证不能证明这些行为，也不自动请求或建立此环境。部署验收须从真实 NAS 入口分别检查登录、WebSocket、缩放、重连及两用户数据归属。构建及原生验证均不使用容器。
