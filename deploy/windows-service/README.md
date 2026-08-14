# AxonHub Windows service

This directory installs AxonHub as a real Windows Service Control Manager
service named `AxonHub`. The service host supervises the backend and, when
available, the Vite frontend. Unexpected child-process exits are restarted and
service failures use Windows recovery actions.

Run these commands from an elevated terminal, or double-click the `.bat`
wrappers; the PowerShell scripts request elevation automatically:

```text
install.bat          Build and install the service
install.bat -Start   Build, install, and start it
install.bat -BackendOnly  Install only the backend service
install.bat -SkipBuild    Reuse existing binaries in .agent\windows-service
start.bat            Start the service
stop.bat             Stop the service and its child processes
restart.bat          Restart the service
status.bat           Show service configuration and state
uninstall.bat        Remove the service (keeps binaries, config, and logs)
```

The generated service files are kept out of Git under
`.agent\windows-service`:

- `axonhub-service.exe` — Windows SCM host
- `axonhub.exe` — backend binary built from this checkout
- `axonhub-service.json` — paths and optional frontend command
- `logs\` — service, backend, and frontend logs

The service runs as `LocalSystem` and uses the repository root as its working
directory, so the existing `config.yml` and SQLite database are used. If the
service is moved to another checkout, reinstall it so the absolute paths in
the service definition are regenerated.

For a production deployment, build the frontend into the backend and use
`install.bat -BackendOnly`. The default development-mode frontend requires
`pnpm.cmd` and Node to be available to the LocalSystem account.
