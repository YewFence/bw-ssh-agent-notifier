# Bitwarden SSH Agent Wrapper 设计草案

## 背景

Bitwarden Desktop Flatpak 作为 SSH agent 时，因为沙盒内无法可靠读取宿主侧调用进程信息，授权弹窗里的应用名称会显示为 `Unknown application`。这个 wrapper 不尝试替代 Bitwarden 的授权，也不尝试修改 SSH agent 协议，只在 Bitwarden agent 前面加一层本机代理，用于记录真实调用方信息并发送桌面通知。

另一个动机：Bitwarden 的授权弹窗可以被设置为「记住本次决定」，之后同类请求不再询问——这正是手滑（例如 `jj git push -b main` 敲错分支）漏过去的时候。wrapper 可选的门禁模式把每次签名确认拿回来，并附上 Bitwarden 给不了的调用进程归因。

## 目标

1. 对外提供一个新的 Unix socket，作为用户 shell 和桌面程序使用的 `SSH_AUTH_SOCK`。
2. 接收 SSH agent 客户端连接后，读取连接方的 Linux peer credentials。
3. 根据 peer `PID` 读取 `/proc/<pid>/exe`、`/proc/<pid>/cmdline`，并尽量记录父进程链。
4. 向 Linux 桌面发送一条通知，提示哪个进程正在访问 SSH agent。
5. 将所有 SSH agent 协议字节透明转发到 Bitwarden Flatpak 的 agent socket。
6. 日志输出到标准输出，由 `systemd --user` 接管，方便通过 `journalctl --user` 查看。
7. 可选门禁模式：拦截签名请求，等待终端控制器允许或拒绝；支持带 TTL 的记忆决策与同源请求批量裁决。

## 非目标

1. 默认模式不解析 SSH agent 协议；门禁模式的协议处理完全委托给 `golang.org/x/crypto/ssh/agent`，不自研 wire format 解析。
2. 金库解锁与密钥保管仍归 Bitwarden；门禁只决定「这次签名放不放行」。
3. 不尝试把真实应用名传给 Bitwarden，因为 SSH agent 协议本身没有调用方应用名字段。
4. 不做密钥指纹级别规则、不做可见性过滤、不做允许列表或拒绝列表，这些可以后续扩展。
5. 不修改 Bitwarden、Flatpak 或系统 SSH 配置。
6. 门禁防的是自己的手滑，不是恶意软件：同 uid 进程可以直连任一 socket，绕过门禁。

## 基本架构

```text
ssh / git / ssh-add
        |
        | SSH_AUTH_SOCK
        v
wrapper unix socket
        |
        | peer credentials + /proc inspection + notify + log
        v
Bitwarden Flatpak ssh-agent socket
        |
        v
Bitwarden authorization popup
```

## 默认路径

wrapper 对外 socket：

```text
$XDG_RUNTIME_DIR/bitwarden-ssh-agent-wrapper.sock
```

XDG_RUNTIME_DIR 缺失时（例如环境被剥掉的 ssh 会话），默认回退到 `/run/user/<uid>/`，两者指向同一个 logind 目录。

Bitwarden Flatpak agent socket：

```text
$HOME/.var/app/com.bitwarden.desktop/data/.bitwarden-ssh-agent.sock
```

如果环境变量里显式提供 `BITWARDEN_SSH_AGENT_SOCKET`，wrapper 优先使用该路径作为上游 socket。

## 进程识别

连接建立后，wrapper 使用 `getsockopt(SO_PEERCRED)` 获取客户端的 `pid`、`uid`、`gid`。

拿到 `pid` 后读取这些信息：

```text
/proc/<pid>/exe
/proc/<pid>/cmdline
/proc/<pid>/status
```

其中 `/proc/<pid>/status` 用于读取 `PPid`，然后继续向上读取有限层级的父进程链，例如最多 5 层，避免日志太吵或遇到异常进程树时循环。

日志里建议记录：

```text
client_pid=12345
client_uid=1000
client_exe=/usr/bin/ssh
client_cmdline=ssh git@github.com
parent_chain=zsh <- foot
upstream_socket=/home/yewfence/.var/app/com.bitwarden.desktop/data/.bitwarden-ssh-agent.sock
```

## 桌面通知

第一版默认直接调用 freedesktop desktop notifications D-Bus 接口，而不是依赖 `notify-send`。这样运行时不需要假设系统安装了 `libnotify` 的命令行工具，也能更准确地区分 session bus 不可用、通知服务不存在、调用超时和通知服务返回错误。

D-Bus 目标：

```text
bus=session
service=org.freedesktop.Notifications
object=/org/freedesktop/Notifications
method=org.freedesktop.Notifications.Notify
```

建议使用 `github.com/godbus/dbus/v5`。这个库是 Go 原生 D-Bus client，不需要 cgo，也不需要链接系统 `libdbus`。通知调用设置短超时，例如 2 秒，避免通知服务异常时影响 SSH agent 转发。

通知标题：

```text
SSH agent request
```

通知正文示例：

```text
ssh is using Bitwarden SSH agent
PID 12345 · /usr/bin/ssh
```

如果 session bus 不存在、通知服务不存在、调用超时、或当前会话不能发通知，wrapper 只写日志，不影响 SSH agent 转发。

`notify-send` 可以作为手动调试手段，但不作为默认实现依赖。它只是 D-Bus 通知协议的命令行前端，不保证所有桌面环境或轻量窗口管理器环境都预装。

## 转发行为

默认模式（`--gate off`）下 wrapper 不解析 SSH agent 消息，只做透明双向复制：

1. 接收客户端 Unix socket 连接。
2. 打开到 Bitwarden agent socket 的 Unix socket 连接。
3. 启动两个 goroutine，分别执行 `client -> upstream` 和 `upstream -> client`。
4. 任意方向结束或报错时关闭两端连接。
5. 每个客户端连接独立处理，允许并发连接。

## 门禁模式

`--gate confirm` 启用门禁。每个客户端连接仍然持有独立的 upstream 连接，但客户端侧不再做字节管道，而是 `agent.ServeAgent(gate.Agent, clientConn)`；upstream 侧用 `agent.NewClient(upstreamConn)` 包装。协议解析全部由 `x/crypto/ssh/agent` 完成，wrapper 只实现 `agent.ExtendedAgent` 接口的策略：

```text
ssh / git / jj
   | SSH_AUTH_SOCK（同一个 wrapper socket）
   v
agent.ServeAgent(gate.Agent)      List 透传；SignWithFlags 拦截裁决
   |                              Add/Remove/Lock/Extension 一律 FAILURE
   v
agent.NewClient(upstream)
   |
   v
Bitwarden sock
```

### 签名请求裁决

`SignWithFlags` 组装待决请求：key 指纹（`ssh.FingerprintSHA256`）、key 注释（实时向上游 List 匹配 blob 得到，失败则只显示指纹）、客户端进程信息（peer credentials + `/proc` 检查，含父链）。请求进入待决队列并广播给所有控制器，同时发送桌面通知作为门铃（同指纹已有待决请求时不重复响）。

失败策略是 fail-closed：

1. 超过 `--gate-timeout`（默认 60s）未裁决则拒绝，并发通知说明原因；没有控制器连接时也正常等待至超时。
2. 请求方连接断开时撤销待决请求，并通知控制器。
3. daemon 关闭时未决请求按 shutdown 拒绝。

### 控制通道

daemon 额外监听一个控制 socket，默认 `$XDG_RUNTIME_DIR/bitwarden-ssh-agent-wrapper-control.sock`（同样在 XDG_RUNTIME_DIR 缺失时回退 `/run/user/<uid>/`），协议为 JSON-lines：

```text
server -> controller: hello / pending / resolved / state / ok / error
controller -> server: {"type":"decide","id":"3","decision":"allow|deny","remember_seconds":60,"scope_index":2}
                 {"type":"list"}
```

`bwsshntfr gate` 是内置控制器，bubbletea TUI：待决列表（key 名、请求者 cmdline、PID、父链、超时倒计时、指纹），键位 `a/y` 允许一次、`m` 允许并记住（弹出 1m/5m/15m/自定义选择器，默认项为全局默认 TTL）、`d/n` 拒绝、`j/k` 或上下键切换请求、`h/l` 或左右键选择授权命令层级、`q` 退出。向左选父进程、向右选请求者，无需 Enter 确认选层；只有裁决动作才发往 daemon。时长选择器保留原有按键。任何实现控制协议的客户端都可以替代它。

请求通过 `scopes` 提供可选命令的完整 argv、exe、PID 和内核 start time，以及 `default_scope`、`scope_automatic`。界面区分请求者与选中的记忆范围，展示选层来源及「此 key、此命令和后代、TTL 到期或命令退出失效」。手动选层按请求 ID 保存，刷新和切换请求不覆盖；时长选择器绑定原请求和所选层级，原请求结束时关闭，不能误操作下一条请求。

### 记忆模型

授权标识 = `key 指纹 + 所选进程 PID + 内核 start time`，绑定「这一次命令」而非同名命令文本。后续请求的可选命令链包含该实例、且实例仍存活时可复用，内部 git/ssh 等子命令参数不参与匹配。进程身份和存活状态通过 `prometheus/procfs` 读取，PID 重用不能继承授权。每次签名前刷新进程信息，避免沿用连接建立时的祖先快照。

shell、复用器（tmux/zellij/screen）、终端模拟器和会话基础设施（systemd/dbus）只显示为上下文，不是可选命令。默认选择最底层请求者；daemon 的可重复参数 `--gate-command-group "bd sync"` 或环境变量 `WRAPPER_GATE_COMMAND_GROUPS="bd sync,jj git push"` 可预设命令组。组内元素由空白分隔，环境变量用逗号分隔多组；逗号和空格不能出现在单个参数中。组内是可执行文件名或绝对路径、后接完整 argv 元素前缀；多组同时匹配时选最近的命令。命令行组替换环境变量组。预设只负责自动选层，绝不自动授权。

属性：

1. 固定窗口不续期；TTL 全局默认 1m，可配，`0` 完全禁用记忆。TTL 到期或所选进程退出，任一发生即失效；同样命令重新执行是新实例。
2. 仅内存，daemon 重启即忘。
3. deny 不写记忆。
4. 记忆命中静默放行（journal 留审计日志），不发通知。
5. 一次允许或拒绝仅批量处理同 key、同请求者实例的待决请求；记住允许则同时处理同 key、被所选命令实例覆盖的全部待决请求，不影响其他 key 或独立命令。
6. 日志记录可选命令及记住时选中的 scope PID、start time、cmdline；进程身份不可读时禁用记住，仍可一次裁决。

### 通知策略

门禁模式下连接通知静默（日志保留）；只发送待决门铃与裁决结果（拒绝/超时含原因）。默认模式维持原有连接通知行为。

## systemd user service

服务文件可以放在：

```text
~/.config/systemd/user/bitwarden-ssh-agent-wrapper.service
```

草案：

```ini
[Unit]
Description=Bitwarden SSH Agent Wrapper
After=graphical-session.target

[Service]
Type=simple
ExecStart=%h/.local/bin/bitwarden-ssh-agent-wrapper
Restart=on-failure
RestartSec=2s
Environment=BITWARDEN_SSH_AGENT_SOCKET=%h/.var/app/com.bitwarden.desktop/data/.bitwarden-ssh-agent.sock

[Install]
WantedBy=default.target
```

可以维护一个示例文件在仓库

shell 中使用：

```sh
export SSH_AUTH_SOCK="$XDG_RUNTIME_DIR/bitwarden-ssh-agent-wrapper.sock"
```

日志查看：

```sh
journalctl --user -u bitwarden-ssh-agent-wrapper.service -f
```

## 错误处理

1. 如果 wrapper socket 已存在，启动时先尝试连接它，能连接说明已有实例在运行，应退出并报错；不能连接说明是陈旧 socket，可以删除后重新监听。
2. 如果 Bitwarden 上游 socket 不存在，服务仍可以启动失败并由 systemd 重启，或者第一版直接退出，交给 `Restart=on-failure`。
3. 如果读取 `/proc` 失败，记录 `pid` 和失败原因，然后继续转发。
4. 如果通知发送失败，只记录日志，不影响转发。
5. 如果上游连接失败，向客户端关闭连接，并记录错误。

## 命令行设计

根命令默认启动 wrapper，`run` 作为显式别名。这样 systemd service 可以直接调用二进制，人手调试时也可以显式写出运行意图。

```text
bwsshntfr
  run
  gate
  doctor
  notify
    test
  debug
    inspect-pid
  systemd
    print-user-service
  completion
    bash
    zsh
    fish
    powershell
  version
```

日常运行：

```sh
bwsshntfr
bwsshntfr run
bwsshntfr run --gate confirm
```

`gate` 连接控制 socket，打开裁决 TUI。控制 socket 不存在时给出明确报错并提示 daemon 是否以门禁模式运行。

`doctor` 只检查环境，不启动代理。检查项包括：

```text
XDG_RUNTIME_DIR 是否存在
wrapper socket 路径是否可写
Bitwarden 上游 socket 是否存在
session D-Bus 是否可连接
org.freedesktop.Notifications 是否可用
/proc 进程信息读取是否可用
```

通知测试：

```sh
bwsshntfr notify test
bwsshntfr notify test --summary "SSH agent request" --body "test notification"
```

`notify test` 只验证 D-Bus 通知通路，不访问 SSH agent socket。

进程识别调试：

```sh
bwsshntfr debug inspect-pid 12345
```

`debug inspect-pid` 打印指定进程的 `exe`、`cmdline`、`uid`、`gid` 和父进程链，用于验证 wrapper 将记录的调用方信息。

systemd service 生成：

```sh
bwsshntfr systemd print-user-service
```

`systemd print-user-service` 只把 user service 内容输出到标准输出，不直接写入用户目录，也不执行 `systemctl`。进程启动、停止和重启交给 systemd 管理，CLI 第一版不提供 `start`、`stop`、`restart`。

全局 flags：

```text
--listen
--upstream
--notify
--notify-timeout
--parent-depth
--gate
--gate-timeout
--gate-remember-ttl
--gate-command-group
--control-socket
--log-level
--log-format
```

配置优先级：

```text
命令行 flag > 环境变量 > 默认值
```

flag 和环境变量映射：

```text
--listen          WRAPPER_SSH_AGENT_SOCKET
--upstream        BITWARDEN_SSH_AGENT_SOCKET
--notify          WRAPPER_NOTIFY_BACKEND
--notify-timeout  WRAPPER_NOTIFY_TIMEOUT
--parent-depth    WRAPPER_PARENT_DEPTH
--gate                  WRAPPER_GATE
--gate-timeout          WRAPPER_GATE_TIMEOUT
--gate-remember-ttl     WRAPPER_GATE_REMEMBER_TTL
--gate-command-group    WRAPPER_GATE_COMMAND_GROUPS
--control-socket        WRAPPER_CONTROL_SOCKET
```

`--notify` 使用枚举值而不是布尔值。第一版支持：

```text
dbus
off
```

默认值为 `dbus`。如果后续增加 `notify-send` fallback，可以扩展为 `dbus`、`notify-send`、`off`。

## 配置项

第一版可以只支持环境变量：

```text
BITWARDEN_SSH_AGENT_SOCKET
WRAPPER_SSH_AGENT_SOCKET
WRAPPER_NOTIFY_BACKEND
WRAPPER_NOTIFY_TIMEOUT
WRAPPER_PARENT_DEPTH
WRAPPER_GATE
WRAPPER_GATE_TIMEOUT
WRAPPER_GATE_REMEMBER_TTL
WRAPPER_GATE_COMMAND_GROUPS
WRAPPER_CONTROL_SOCKET
```

默认值：

```text
BITWARDEN_SSH_AGENT_SOCKET=$HOME/.var/app/com.bitwarden.desktop/data/.bitwarden-ssh-agent.sock
WRAPPER_SSH_AGENT_SOCKET=$XDG_RUNTIME_DIR/bitwarden-ssh-agent-wrapper.sock
WRAPPER_NOTIFY_BACKEND=dbus
WRAPPER_NOTIFY_TIMEOUT=2s
WRAPPER_PARENT_DEPTH=5
WRAPPER_GATE=off
WRAPPER_GATE_TIMEOUT=60s
WRAPPER_GATE_REMEMBER_TTL=1m
WRAPPER_GATE_COMMAND_GROUPS=""
WRAPPER_CONTROL_SOCKET=$XDG_RUNTIME_DIR/bitwarden-ssh-agent-wrapper-control.sock
```

## Go 实现建议

优先使用标准库：

```text
net.ListenUnix
net.DialUnix
syscall.GetsockoptUcred
os.Readlink
os.ReadFile
io.Copy
context.WithTimeout
log/slog
```

桌面通知使用：

```text
github.com/godbus/dbus/v5
```

门禁模式使用：

```text
golang.org/x/crypto/ssh/agent   # agent 协议两头（ServeAgent / NewClient）
github.com/charmbracelet/bubbletea  # gate 子命令的 TUI
github.com/charmbracelet/lipgloss   # TUI 样式
```

如果需要更干净的 Linux syscall 封装，可以考虑 `golang.org/x/sys/unix`，但第一版标准库已经足够。

## 后续扩展

1. 增加简单允许列表，只通知未知进程。
2. REQUEST_IDENTITIES 可见性过滤（隐藏归档 key，对齐 EnhydraV/bw-ssh-agent-filter 的能力）。
3. deny 侧记忆（拒绝并静音一段时间）。
4. 包装进程剥离名单开放为配置（当前硬编码集中在 `internal/process`）。
5. 增加 `notify-send` fallback，方便在 D-Bus 调试困难时对比问题。
6. 增加 socket activation，让 systemd 按需启动 wrapper。

## 验证步骤

1. 启动 Bitwarden Desktop Flatpak，并确认 SSH agent 已启用。
2. 启动 wrapper，并确认 wrapper socket 已创建。
3. 设置 `SSH_AUTH_SOCK` 指向 wrapper socket。
4. 执行 `ssh-add -L`，确认 Bitwarden 授权弹窗仍出现。
5. 查看桌面通知，确认显示调用方进程。
6. 查看 `journalctl --user -u bitwarden-ssh-agent-wrapper.service -f`，确认日志里有 `pid`、`exe`、`cmdline` 和父进程链。
7. 以 `--gate confirm` 重启 wrapper，打开 `bwsshntfr gate`，执行一次 `ssh` 或 `git push`：
   - 不开 TUI 时签名等待至 gate-timeout 后被拒，通知说明 timeout。
   - 开 TUI 但不裁决，超过 gate-timeout 后被拒，通知说明 timeout。
   - TUI 按 `a` 一次放行；用 `h/l` 选择发起命令实例，按 `m` 选 1m，随后该实例下不同内部子命令使用同 key 不再询问（journal 出现 allowed by remembered decision）。所选命令退出或 TTL 到期后失效；同样命令重新启动需要重新确认。
   - 换一个分支名重复执行，重新弹出询问。
