# Capacity Runner

Run from `apps/gateway`:

```powershell
.\scripts\capacity-runner.ps1 -Profile .\scripts\load-profile.json -DryRun
```

With tags:

```powershell
.\scripts\capacity-runner.ps1 -Profile .\scripts\load-profile.json -Tag baseline
.\scripts\capacity-runner.ps1 -Profile .\scripts\load-profile.json -Tag improved
```

Results are written to:

`apps/gateway/scripts/capacity/results/<timestamp>-<tag>.json`
