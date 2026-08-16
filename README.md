# xboard-node

Node backend for [Xboard](https://github.com/cedar2025/Xboard). Supports `sing-box` / `xray-core` dual kernels.

> **Disclaimer**: This project is for educational and learning purposes only.

## Features

- Protocols: V2Ray family, Trojan, Shadowsocks, Hysteria2, TUIC, AnyTLS
- Sync: WebSocket push + REST polling dual channel
- User controls: speed limit, device limit, alive-IP tracking, hot update
- Deploy modes: node mode, machine mode, standalone mode
- Multi-instance: single process binding multiple panels / nodes

## Install

### Docker

```bash
docker run -d --restart=always --network=host \
  -e apiHost=https://panel.com -e apiKey=TOKEN -e nodeID=1 \
  ghcr.io/cedar2025/xboard-node:latest
```

### Docker Compose

```bash
git clone -b compose --depth 1 https://github.com/miyling/Xboard-Node.git
cd xboard-node
vim config/config.yml   # set panel.url / token / node_id
docker compose up -d
```

### Installer (Linux systemd)

```bash
# Node mode
curl -fsSL https://raw.githubusercontent.com/miyling/Xboard-Node/dev/install.sh | \
  sudo bash -s -- --mode node --panel https://panel.example.com --token TOKEN --node-id 1

# Machine mode
curl -fsSL https://raw.githubusercontent.com/miyling/Xboard-Node/dev/install.sh | \
  sudo bash -s -- --mode machine --panel https://panel.example.com --token TOKEN --machine-id 1
```

### Installer (Windows Server)

Open PowerShell as Administrator. The installer downloads the Windows `.exe`
artifacts, stores configuration under `%ProgramData%\xboard-node`, installs
the native `xboard-node` Windows Service, and writes logs to
`%ProgramData%\xboard-node\logs\xboard-node.log`.

```powershell
Set-ExecutionPolicy -Scope Process Bypass
Invoke-WebRequest https://raw.githubusercontent.com/miyling/Xboard-Node/dev/install.ps1 -OutFile .\install.ps1
Invoke-WebRequest https://raw.githubusercontent.com/miyling/Xboard-Node/dev/scripts/windows-smoke.ps1 -OutFile .\windows-smoke.ps1

# One-line node installation (run in an elevated PowerShell)
& ([scriptblock]::Create((Invoke-WebRequest -UseBasicParsing -Uri 'https://raw.githubusercontent.com/miyling/Xboard-Node/dev/install.ps1').Content)) -Mode node -Panel https://panel.example.com -Token TOKEN -NodeId 1

# Node mode
& .\install.ps1 -Mode node -Panel https://panel.example.com -Token TOKEN -NodeId 1

# Machine mode
& .\install.ps1 -Mode machine -Panel https://panel.example.com -Token TOKEN -MachineId 1

# Upgrade / uninstall
& .\install.ps1 -Action upgrade
& .\install.ps1 -Action uninstall -Yes
```

Windows service operations are available through `xbctl.exe service
<status|start|stop|restart|enable|disable|logs>`. The service command line
contains only absolute config and credentials file paths; credentials are not
placed in the Windows Service definition.

On a Windows Server test host, the lifecycle smoke check can be run after
installation with `.\windows-smoke.ps1 -HealthPort 65530`.

## xbctl

Run `xbctl` after installation for help. Common commands:

```bash
xbctl list                          # list all instances
xbctl status                        # running status
xbctl bind add-node --panel URL --token TOKEN --node-id 1
xbctl bind add-machine --panel URL --token TOKEN --machine-id 1
xbctl bind remove-node --panel URL --node-id 1
xbctl service restart
```

## Configuration

Legacy single-panel config is fully compatible. Appending bindings auto-migrates to `instances` format. See `config.yml.example`.

## Extensions

- Custom routes: [docs-custom-routes.md](docs-custom-routes.md)
- Custom outbounds: [docs-custom-outbounds.md](docs-custom-outbounds.md)
- DNS providers (ACME DNS-01): [docs-dns-providers.md](docs-dns-providers.md)

## License

MPL-2.0.
