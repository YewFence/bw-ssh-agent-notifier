# Bitwarden SSH Agent Wrapper 设计草案

## 背景

Bitwarden Desktop Flatpak 作为 SSH agent 时，因为沙盒内无法可靠读取宿主侧调用进程信息，授权弹窗里的应用名称会显示为 `Unknown application`。这个 wrapper 不尝试替代 Bitwarden 的授权，也不尝试修改 SSH agent 协议，只在 Bitwarden agent 前面加一层本机代理，用于记录真实调用方信息并发送桌面通知。

## 目标

1. 对外提供一个新的 Unix socket，作为用户 shell 和桌面程序使用的 `SSH_AUTH_SOCK`。
2. 接收 SSH agent 客户端连接后，读取连接方的 Linux peer credentials。
3. 根据 peer `PID` 读取 `/proc/<pid>/exe`、`/proc/<pid>/cmdline`，并尽量记录父进程链。
4. 向 Linux 桌面发送一条通知，提示哪个进程正在访问 SSH agent。
5. 将所有 SSH agent 协议字节透明转发到 Bitwarden Flatpak 的 agent socket。
6. 日志输出到标准输出，由 `systemd --user` 接管，方便通过 `journalctl --user` 查看。

## 非目标

1. 不实现 SSH agent 协议解析。
2. 不在 wrapper 内做授权确认，授权仍然交给 Bitwarden。
3. 不尝试把真实应用名传给 Bitwarden，因为 SSH agent 协议本身没有调用方应用名字段。
4. 不做密钥指纹级别规则、不做允许列表、不做拒绝列表，这些可以后续扩展。
5. 不修改 Bitwarden、Flatpak 或系统 SSH 配置。

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

wrapper 不解析 SSH agent 消息，只做透明双向复制：

1. 接收客户端 Unix socket 连接。
2. 打开到 Bitwarden agent socket 的 Unix socket 连接。
3. 启动两个 goroutine，分别执行 `client -> upstream` 和 `upstream -> client`。
4. 任意方向结束或报错时关闭两端连接。
5. 每个客户端连接独立处理，允许并发连接。

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
```

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
```

默认值：

```text
BITWARDEN_SSH_AGENT_SOCKET=$HOME/.var/app/com.bitwarden.desktop/data/.bitwarden-ssh-agent.sock
WRAPPER_SSH_AGENT_SOCKET=$XDG_RUNTIME_DIR/bitwarden-ssh-agent-wrapper.sock
WRAPPER_NOTIFY_BACKEND=dbus
WRAPPER_NOTIFY_TIMEOUT=2s
WRAPPER_PARENT_DEPTH=5
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

如果需要更干净的 Linux syscall 封装，可以考虑 `golang.org/x/sys/unix`，但第一版标准库已经足够。

## 后续扩展

1. 增加简单允许列表，只通知未知进程。
2. 解析 SSH agent request，记录签名请求和 key fingerprint。
3. 支持交互确认，但默认仍交给 Bitwarden 授权。
4. 增加 `notify-send` fallback，方便在 D-Bus 调试困难时对比问题。
5. 增加 socket activation，让 systemd 按需启动 wrapper。

## 验证步骤

1. 启动 Bitwarden Desktop Flatpak，并确认 SSH agent 已启用。
2. 启动 wrapper，并确认 wrapper socket 已创建。
3. 设置 `SSH_AUTH_SOCK` 指向 wrapper socket。
4. 执行 `ssh-add -L`，确认 Bitwarden 授权弹窗仍出现。
5. 查看桌面通知，确认显示调用方进程。
6. 查看 `journalctl --user -u bitwarden-ssh-agent-wrapper.service -f`，确认日志里有 `pid`、`exe`、`cmdline` 和父进程链。
