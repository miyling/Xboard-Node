# Windows Server cross-platform support

## Goal

让 xboard-node 能在 Windows Server 上以可维护、可自动启动的方式运行，同时保留现有 Linux 部署与节点/机器/多实例能力。Windows 版本应继续使用现有 Xboard 面板协议和 sing-box/xray 内嵌内核，不要求面板侧做分叉。

## Background and confirmed facts

- 这是 Go 单体后端；xboard-node 直接嵌入 sing-box/xray 内核，不依赖 Linux 上的外部内核进程（`README.md:1-13`）。
- 当前发布与安装路径只覆盖 Linux：README 将安装方式标记为 Linux systemd（`README.md:34-43`），Makefile 只生成 Linux amd64/arm64 产物（`Makefile:12-23`）。
- `install.sh` 固定使用 `/etc/xboard-node`、`/usr/local/bin`、systemd、journalctl 和 Linux 命令（`install.sh:11-30`, `install.sh:320-358`, `install.sh:591-689`）。
- `xbctl` 固定使用 Linux 文件路径和 systemd service，并通过 `sudo`/`systemctl`/`journalctl` 管理生命周期（`cmd/xbctl/main.go:24-33`, `cmd/xbctl/main.go:292-311`）；升级产物名称也固定为 Linux 架构（`cmd/xbctl/main.go:384-472`）。
- 核心配置加载、网络、文件监听和证书逻辑大部分使用 Go 标准库或跨平台库，但仍存在 Linux 默认路径、以 `/` 查询磁盘用量，以及只处理 `Write/Create` 文件事件等跨平台风险（`internal/config/config.go:300-309`, `internal/monitor/monitor.go:128-151`, `internal/config/watcher.go:96-121`）。
- 当前工作区没有 Go 工具链，无法在本机直接执行 `go test` 或 Windows 交叉编译；验证计划需要使用可复现的 Go/Docker/CI 环境。

## Requirements

- Windows Server 可获得可执行的 `xboard-node.exe` 和 `xbctl.exe`，至少覆盖 amd64；Linux 现有构建产物不回归。
- 配置、凭据、证书、GeoIP/GeoSite、缓存和日志使用 Windows 合法路径，并支持通过配置文件启动；多实例目录不能冲突。
- Windows 服务模式能正确响应启动、停止、重启和系统服务控制，支持开机自启、故障自动恢复，并保留控制台前台运行用于调试。
- `xbctl` 的 status/list/health/bind/config/service/upgrade/uninstall 等与 Windows 相关的操作不再调用 Linux 专属命令；凭据不能依赖 systemd 的 `EnvironmentFile` 才能生效。
- 配置热加载、证书/GeoData 原子写入、日志输出和资源监控在 Windows 上有明确且可验证的行为。
- README、配置示例、构建目标和 Windows 安装/升级/卸载说明同步更新。

## Key scope decision

本任务采用完整原生 Windows Server 部署范围：交付 Windows `.exe` 产物、Windows Service 生命周期管理、PowerShell 安装/升级/卸载流程和 Windows 版 `xbctl`；Linux 既有构建与 systemd 流程继续保留。

## Acceptance Criteria

- [ ] Windows 支持边界（运行时、服务管理、安装升级、架构）已确定并写入最终计划。
- [ ] Windows Server 上可按文档完成安装、配置、启动、健康检查、停止、升级和卸载；Linux 现有流程保持可用。
- [ ] Windows amd64 交叉编译、单元测试和静态检查在可复现环境中通过。
- [ ] Windows 专属行为有自动化测试或明确的构建/验证脚本，且不依赖当前开发机必须运行 Windows。
