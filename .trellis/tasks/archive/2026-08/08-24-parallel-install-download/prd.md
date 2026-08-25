# 加速 install.ps1 下载

## Goal

缩短 Windows 安装/升级等待时间。`install.ps1` 应同时启动两个相互独立的
Windows 发布包下载，而不是等待第一个文件完成后才开始第二个文件。

## Background and confirmed facts

- `Install-OrUpgrade` 当前在 `install.ps1:228-229` 依次下载
  `xboard-node-windows-<arch>.exe` 和 `xbctl-windows-<arch>.exe`。
- 下载完成后才会执行配置生成、备份、替换文件和服务重启；这部分顺序和
  回滚契约不能改变。
- `-Binary` 和 `-XbctlBinary` 支持用户提供本地文件，不能因并发下载改动其
  复制行为。
- Windows 部署契约要求兼容 Windows Server，并且当前脚本面向 Windows
  PowerShell 5.1；不能依赖 PowerShell 7 的 `ForEach-Object -Parallel`。
- 仓库当前没有 aria2 依赖或安装约定；`aria2c` 不是 Windows Server 的系统
  内置命令，因此不能把它作为首次安装的硬前置条件。
- 两个远程发布包均来自现有 `Resolve-DownloadUrl` 逻辑；发布 URL、架构选择、
  版本参数和文件名契约不变。

## Requirements

1. 对需要远程下载的发布包，若系统 PATH 中存在 `aria2c`，使用 aria2 的多
   连接能力（单文件分段连接，并发处理两个资产）；若不存在，则使用
   Windows 自带/.NET 能力并发启动下载任务。两种路径都不得影响本地文件输入。
2. 等待所有已启动的下载任务完成后，才进入现有配置和安装流程；任一下载
   失败都必须中止本次安装，并沿用现有 staging 清理与错误传播行为。
3. 下载客户端、任务和临时资源必须在成功或失败路径释放；不得留下可被后续
   安装误用的半成品 staging 文件。
4. 不改变现有安装/升级、回滚、卸载、服务、凭据保护和健康检查行为。
5. 保持脚本可被 Windows PowerShell 5.1 执行，并保留现有下载日志的可读性。

## Acceptance Criteria

- [x] 当两个二进制均使用远程 URL 且存在 `aria2c` 时，脚本使用配置的多连接
      下载，并在两个下载完成后才继续配置/安装。
- [x] 当 `aria2c` 不存在时，第二个远程下载会在第一个完成前启动，且两个
      下载完成后才继续配置/安装。
- [x] 当一个或两个二进制通过 `-Binary` / `-XbctlBinary` 提供时，本地复制
      仍然有效，远程的另一个文件仍可独立下载。
- [x] 任一远程下载失败时，脚本返回失败，不进入服务替换；临时目录会按现有
      `finally` 路径清理。
- [x] 版本、架构、发布 URL 和两个目标文件名与改动前一致。
- [ ] PowerShell 语法检查通过；可用时运行 Windows/PowerShell 相关 smoke 或
      静态验证，且现有 Go 测试不受影响。（当前 macOS 环境没有
      PowerShell/Go；`git diff --check` 和 JSONL 校验已通过，Windows 验证待
      在 Windows 主机执行。）

## Out of scope

- 将单个大文件拆成 HTTP Range 分片并发下载。
- 自动安装第三方下载器、PowerShell 7-only API、断点续传或新的命令行参数。
- 修改 GitHub Release 资产、下载 URL、安装目录、回滚策略或 README 安装协议。

## Risks and deferred items

- aria2 不存在时，.NET `WebClient` 异步下载可覆盖 Windows PowerShell 5.1，
  且会继续遵循现有 Release URL 重定向；若目标环境的代理/TLS 行为与
  `Invoke-WebRequest` 不同，需要在 Windows Server 上做实际 smoke 验证。
- aria2 可用时，其代理、TLS、重定向和服务器 Range 支持可能与系统下载栈不同；
  需要检查非零退出码并保留内置回退路径。
- 并发粒度限定为两个独立资产；如果后续证明瓶颈来自单个资产带宽，再单独评估
  分片下载。
