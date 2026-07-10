# CampusLink

CampusLink 是一个校园网 SRun 自动登录工具。实现语言为 Go，产物是单文件二进制程序，不依赖脚本运行时、包管理器或虚拟环境。当前默认面向 `nap.cug.edu.cn`。

## 下载和安装

GitHub Actions 会为以下目标系统编译二进制产物：

| 系统 | 架构 | Artifact |
| --- | --- | --- |
| Linux | x64 / amd64 | `campuslink-linux-amd64.tar.gz` |
| Windows | x64 / amd64 | `campuslink-windows-amd64.zip` |
| macOS | arm64 / Apple Silicon | `campuslink-darwin-arm64.tar.gz` |

从 [GitHub Releases](https://github.com/CUG-CS-632/CampusLink/releases/latest) 下载对应系统的压缩包。解压后得到：

- Windows: `campuslink.exe`
- Linux/macOS: `campuslink`

推荐安装位置：

| 系统 | 推荐路径 |
| --- | --- |
| Windows | `%LOCALAPPDATA%\CampusLink\campuslink.exe` |
| Linux | `$HOME/.local/bin/campuslink` |
| macOS | `$HOME/.local/bin/campuslink` |

Windows PowerShell:

```powershell
$InstallDir = "$env:LOCALAPPDATA\CampusLink"
New-Item -ItemType Directory -Force -Path $InstallDir
Expand-Archive .\campuslink-windows-amd64.zip -DestinationPath . -Force
Copy-Item .\campuslink-windows-amd64\campuslink.exe "$InstallDir\campuslink.exe" -Force
```

Linux:

```bash
mkdir -p "$HOME/.local/bin"
tar -xzf campuslink-linux-amd64.tar.gz
install -m 0755 campuslink-linux-amd64/campuslink "$HOME/.local/bin/campuslink"
```

macOS:

```bash
mkdir -p "$HOME/.local/bin"
tar -xzf campuslink-darwin-arm64.tar.gz
install -m 0755 campuslink-darwin-arm64/campuslink "$HOME/.local/bin/campuslink"
```

如果 `campuslink` 命令无法直接找到，可以使用完整路径 `$HOME/.local/bin/campuslink`，或把 `$HOME/.local/bin` 加入 `PATH`。

也可以从源码本地编译安装：

```bash
go build -trimpath -ldflags="-s -w" -o "$HOME/.local/bin/campuslink" ./cmd/campuslink
```

## 快速使用

先手动验证账号、密码和认证参数：

```bash
"$HOME/.local/bin/campuslink" -u "学号" -p "密码"
```

Windows PowerShell:

```powershell
& "$env:LOCALAPPDATA\CampusLink\campuslink.exe" -u "学号" -p "密码"
```

成功时普通模式会输出：

```text
login ok
```

需要查看网关原始响应时再加 `--json`：

```bash
"$HOME/.local/bin/campuslink" -u "学号" -p "密码" --json
```

也可以使用环境变量，避免把密码写进命令历史：

```bash
export SRUN_USERNAME="学号"
export SRUN_PASSWORD="密码"
"$HOME/.local/bin/campuslink"
```

还可以从标准输入读取密码，便于对接密码管理器或其他不会把密码放进进程参数的工具：

```bash
read -rsp "Campus network password: " CAMPUSLINK_PASSWORD
printf '%s\n' "$CAMPUSLINK_PASSWORD" | env -u SRUN_PASSWORD "$HOME/.local/bin/campuslink" -u "学号" --password-stdin
unset CAMPUSLINK_PASSWORD
```

查看当前版本：

```bash
"$HOME/.local/bin/campuslink" --version
```

Release 和 GitHub Actions 构建会同时显示版本号与 commit，例如 `campuslink v1.2.3 (commit abcdef123456)`。本地源码构建会读取 Go 嵌入的 Git 信息；工作区存在未提交改动时，commit 后会附加 `-dirty`。

可配置项：

| 环境变量 | 命令行参数 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `SRUN_USERNAME` | `-u, --username` | 无 | 校园网账号 |
| `SRUN_PASSWORD` | `-p, --password` | 无 | 校园网密码 |
| 无 | `--password-stdin` | 关闭 | 从标准输入读取密码，不能与其他密码来源同时使用 |
| `SRUN_IP` | `--ip` | 自动发现 | 自动发现失败时手动指定客户端 IP |
| `SRUN_BASE_URL` | `--base-url` | `http://nap.cug.edu.cn` | SRun 网关完整基础 URL，优先于 `SRUN_HOST`，支持 HTTP/HTTPS |
| `SRUN_HOST` | `--host` | `http://nap.cug.edu.cn` | 兼容旧配置；可以是网关域名、IP 或完整基础 URL |
| `SRUN_AC_ID` | `--ac-id` | `1` | SRun 接入点 ID |
| `SRUN_TIMEOUT` | `--timeout` | `8` | HTTP 超时时间，单位秒，有效范围 1–3600；非法配置会直接报错 |

如果网关提供有效的 HTTPS 服务，建议明确使用 HTTPS：

```bash
"$HOME/.local/bin/campuslink" --base-url "https://nap.cug.edu.cn"
```

没有协议前缀的 `--host` 值会按 HTTP 处理，以兼容原有配置。客户端会校验 URL、手动指定的 IP 和超时范围；网关响应最多读取 1 MiB，避免异常响应占用过多内存。

## 自动连接方案

推荐方案是：系统启动或用户登录后立即执行一次，然后每 5 分钟重试一次。CampusLink 把“已经在线”视为成功，所以重复执行是安全的；这种方式也能覆盖 Wi-Fi 切换、睡眠恢复、网关踢线等情况。

下面的示例使用上文的推荐安装路径。如果你安装到了其他位置，把服务配置里的程序路径替换成自己的实际路径。

### Windows

Windows 推荐使用任务计划程序。先在 PowerShell 中保存用户级环境变量：

```powershell
[Environment]::SetEnvironmentVariable("SRUN_USERNAME", "学号", "User")
[Environment]::SetEnvironmentVariable("SRUN_PASSWORD", "密码", "User")
```

关闭并重新打开 PowerShell，确认能手动登录：

```powershell
& "$env:LOCALAPPDATA\CampusLink\campuslink.exe"
```

创建登录后执行一次的任务：

```powershell
$CampusLink = "$env:LOCALAPPDATA\CampusLink\campuslink.exe"
schtasks /Create /TN "CampusLink SRun Login" /SC ONLOGON /DELAY 0001:00 /TR "`"$CampusLink`"" /RL LIMITED /F
```

创建每 5 分钟补偿重试的任务：

```powershell
$CampusLink = "$env:LOCALAPPDATA\CampusLink\campuslink.exe"
schtasks /Create /TN "CampusLink SRun Login Retry" /SC MINUTE /MO 5 /TR "`"$CampusLink`"" /RL LIMITED /F
```

立即测试任务：

```powershell
schtasks /Run /TN "CampusLink SRun Login"
```

删除任务：

```powershell
schtasks /Delete /TN "CampusLink SRun Login" /F
schtasks /Delete /TN "CampusLink SRun Login Retry" /F
```

### Linux

Linux 推荐使用 systemd 用户服务和 timer，不需要 root，适合大多数桌面发行版。

保存账号配置：

```bash
mkdir -p ~/.config/campuslink
cat > ~/.config/campuslink/env <<'EOF'
SRUN_USERNAME=学号
SRUN_PASSWORD=密码
EOF
chmod 600 ~/.config/campuslink/env
```

创建服务文件 `~/.config/systemd/user/campuslink-login.service`：

```ini
[Unit]
Description=CampusLink SRun login

[Service]
Type=oneshot
EnvironmentFile=%h/.config/campuslink/env
ExecStart=%h/.local/bin/campuslink
```

创建定时器 `~/.config/systemd/user/campuslink-login.timer`：

```ini
[Unit]
Description=Run CampusLink SRun login periodically

[Timer]
OnBootSec=1min
OnUnitActiveSec=5min
Unit=campuslink-login.service

[Install]
WantedBy=timers.target
```

启用并立即启动：

```bash
systemctl --user daemon-reload
systemctl --user enable --now campuslink-login.timer
systemctl --user start campuslink-login.service
```

查看状态和日志：

```bash
systemctl --user status campuslink-login.service
journalctl --user -u campuslink-login.service -n 50
```

如果希望未登录桌面会话时也能运行用户服务，可以启用 linger：

```bash
sudo loginctl enable-linger "$USER"
```

如果你的 Linux 使用 NetworkManager，并且更希望“网络连上就立刻登录”，可以额外做 dispatcher 脚本；但这需要 root，并且不同发行版路径略有差异。systemd timer 更通用，也更容易排错。

### macOS

macOS 推荐使用 LaunchAgent。先创建日志目录：

```bash
mkdir -p ~/Library/Logs/CampusLink
```

创建 `~/Library/LaunchAgents/io.github.campuslink.srun-login.plist`。下面的命令会把当前用户的 `$HOME` 写入 plist，避免手动替换 `/Users/...` 路径：

```bash
cat > ~/Library/LaunchAgents/io.github.campuslink.srun-login.plist <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>io.github.campuslink.srun-login</string>

  <key>ProgramArguments</key>
  <array>
    <string>$HOME/.local/bin/campuslink</string>
  </array>

  <key>EnvironmentVariables</key>
  <dict>
    <key>SRUN_USERNAME</key>
    <string>学号</string>
    <key>SRUN_PASSWORD</key>
    <string>密码</string>
  </dict>

  <key>RunAtLoad</key>
  <true/>
  <key>StartInterval</key>
  <integer>300</integer>

  <key>StandardOutPath</key>
  <string>$HOME/Library/Logs/CampusLink/stdout.log</string>
  <key>StandardErrorPath</key>
  <string>$HOME/Library/Logs/CampusLink/stderr.log</string>
</dict>
</plist>
EOF
```

设置权限、加载并立即执行：

```bash
chmod 600 ~/Library/LaunchAgents/io.github.campuslink.srun-login.plist
launchctl bootstrap "gui/$(id -u)" ~/Library/LaunchAgents/io.github.campuslink.srun-login.plist
launchctl kickstart -k "gui/$(id -u)/io.github.campuslink.srun-login"
```

查看日志：

```bash
tail -n 50 ~/Library/Logs/CampusLink/stdout.log
tail -n 50 ~/Library/Logs/CampusLink/stderr.log
```

卸载：

```bash
launchctl bootout "gui/$(id -u)" ~/Library/LaunchAgents/io.github.campuslink.srun-login.plist
```

## GitHub CI

- `go test ./...` 验证认证算法、JSONP 解析、CLI 行为和模拟网关登录流程。
- `go vet ./...` 做基础静态检查。
- `go test -race ./...` 验证并发访问安全性；模拟网关测试覆盖 IP 发现、challenge、登录结果、超时、响应上限和错误脱敏。
- `.github/workflows/build.yml` 用于普通 push、PR 和手动触发，交叉编译 Linux x64、Windows x64、macOS arm64，并上传 Actions artifacts。
- `.github/workflows/release.yml` 只负责发版。推送 `v*` tag 会自动创建 GitHub Release，并上传三个系统的压缩包。
- Release workflow 也支持手动触发，输入已有 tag 后会重新构建并补发对应 Release assets。

## 安全说明

- 不要把账号密码提交到 Git。
- 命令行 `-p, --password` 可能出现在 shell 历史和进程列表中；交互使用优先选择环境变量，接入密码管理器时优先选择 `--password-stdin`。
- Windows 用户级环境变量、Linux `EnvironmentFile`、macOS plist 都会在本机保存明文密码；请确保电脑账号本身有登录密码，并限制配置文件权限。
- macOS plist 中的账号或密码如果包含 `&`、`<`、`>` 等字符，需要进行 XML 转义，否则 plist 无法加载。
- 网络错误只记录请求路径和底层原因，不记录包含认证参数的完整 URL；`--json` 会主动输出网关原始响应，请避免把调试输出发送到公共日志。
- 如果需要更强的凭据保护，后续可以扩展为从 Windows Credential Manager、Linux Secret Service 或 macOS Keychain 读取密码。

## 故障排查

- `cannot find client ip from portal page; pass --ip`：网关页面没有返回工具预期的 IP 字段，通常需要重新向 DHCP 申请 IP。重新获取地址后再运行 CampusLink；如果仍失败，可以使用 `--ip` 或 `SRUN_IP` 手动指定。

  Windows PowerShell:

  ```powershell
  ipconfig /release
  ipconfig /renew
  ```

  Linux NetworkManager:

  ```bash
  nmcli networking off
  nmcli networking on
  ```

  macOS:

  ```bash
  sudo ifconfig en0 down
  sudo ifconfig en0 up
  ```

- 无法登录时，先排查代理问题：关闭系统代理、浏览器代理、VPN、TUN 模式或透明代理，确认校园网网关流量没有被代理接管。
- `portal connection failed`：通常是未连接到校园网、DNS 不通、网关域名不可达或认证地址不是默认 `nap.cug.edu.cn`。如果是 DNS 等问题导致无法访问默认网关，可以使用备用地址：

  ```bash
  "$HOME/.local/bin/campuslink" --host 192.168.167.115
  "$HOME/.local/bin/campuslink" --host 192.168.167.116
  ```

  Windows PowerShell:

  ```powershell
  & "$env:LOCALAPPDATA\CampusLink\campuslink.exe" --host 192.168.167.115
  & "$env:LOCALAPPDATA\CampusLink\campuslink.exe" --host 192.168.167.116
  ```

- `login failed`：使用 `--json` 查看网关返回的原始错误，再确认账号、密码、`ac_id` 和学校网关地址。

## 许可证

CampusLink 使用 [MIT License](LICENSE)。
