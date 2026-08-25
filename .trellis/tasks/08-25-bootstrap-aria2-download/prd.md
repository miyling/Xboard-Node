# 临时引导 aria2 加速下载

## Goal

在 Windows amd64 主机未安装 `aria2c` 时，安装器自动获取一个临时、校验过的
aria2 实例，继续使用多连接下载两个 xboard 发布包；安装结束后不在系统中留下
aria2 文件或 PATH 配置。

## Background and confirmed facts

- 当前 [install.ps1](../../../install.ps1) 只通过 PATH 查找 `aria2c.exe`；找不到
  时使用 PowerShell 5.1/.NET 并发下载。
- 现有 aria2 分支已使用两个进程分别下载节点和 CLI，并保留 staging 目录、
  服务安装及回滚顺序。
- 官方 `aria2/aria2` `release-1.37.0` 提供
  `aria2-1.37.0-win-64bit-build1.zip`，压缩包内包含 `aria2c.exe`。
- 该固定资产 SHA-256 为
  `67d015301eef0b612191212d564c5bb0a14b5b9c4796b76454276a4d28d9b288`。
- 官方 release 资产没有 Windows ARM64 版本；ARM64 主机必须继续走现有 .NET
  下载回退，不能下载 x64 可执行文件。
- 仓库没有 aria2 包管理器依赖，也不能把 aria2 永久安装到系统目录。

## Requirements

1. 当 PATH 中找不到 `aria2c.exe` 且目标架构为 `amd64` 时，从固定官方 URL
   下载 aria2 zip 到本次安装专用的临时目录。
2. 使用 `Get-FileHash -Algorithm SHA256` 校验固定摘要；下载失败、解压失败、
   摘要不匹配或找不到压缩包内的 `aria2c.exe` 时，不得执行未经验证的文件。
3. 将 aria2 解压到同一唯一临时目录，复用现有 aria2 并发下载分支；不修改
   系统 PATH、Program Files、注册表或 Windows 服务。
4. 无论安装成功、下载失败、配置失败还是用户中断，都清理临时 zip、解压目录
   和 bootstrap 产生的 aria2 进程；若清理失败，只能发出警告，不能覆盖原始
   安装错误。
5. 当系统已有 aria2 时继续优先使用系统版本；当架构为 ARM64、bootstrap
   不可用或校验失败时，回退现有 .NET 并发下载，不影响安装的基本可用性。
6. 保留现有 URL、版本、发布包文件名、本地 `-Binary` 参数、staging 清理、
   凭据保护、服务流程和回滚行为。

## Acceptance Criteria

- [x] amd64 且 PATH 无 aria2 时，脚本下载固定 zip、校验 SHA-256、解压并使用
      临时 `aria2c.exe` 完成两个资产的并发/分段下载。
- [x] PATH 已有 aria2 时，不重复下载 bootstrap 包。
- [x] ARM64 或 bootstrap 失败时，脚本不执行错误架构的 aria2，转用现有 .NET
      并发下载。
- [x] 摘要不匹配或压缩包缺少 `aria2c.exe` 时，临时文件被清理，未验证的
      aria2 不会启动，并能继续回退或返回明确下载错误。
- [x] bootstrap 资源不写入系统安装目录、PATH、注册表或服务定义。
- [x] 安装成功和失败路径均清理 bootstrap 临时资源；现有服务替换/回滚顺序
      不变。
- [ ] PowerShell 语法检查通过；可用时完成 Windows amd64/ARM64 分支 smoke；
      当前 macOS 环境缺少 PowerShell/Go 时，明确记录验证限制。

## Out of scope

- 永久安装 aria2、修改用户或系统 PATH、注册 Windows 服务。
- 自动使用 winget、Chocolatey、Scoop 或其他包管理器。
- 为 Windows ARM64 编译或引入第三方 ARM64 aria2 构建。
- 动态追踪 aria2 最新版本、下载未固定摘要的可执行文件或改变现有
  xboard-node Release 下载 URL。

## Key decisions and risks

- 固定官方版本和 SHA-256，牺牲自动获取最新 aria2 的便利，换取可审计性和避免
  执行被篡改的 bootstrap 文件。
- bootstrap 只作为 amd64 性能增强；不让 bootstrap 成为安装硬依赖，减少代理、
  GitHub 暂时不可用或架构不匹配对安装成功率的影响。
- GitHub Release 下载和 SHA-256 校验仍需在 Windows Server 上验证代理/TLS、
  `Expand-Archive` 行为及 Defender 对临时可执行文件的处理。
