# Progress

Source of truth: [SPEC.md](SPEC.md). Decisions: [DECISIONS.md](DECISIONS.md).

## Milestones

| # | Milestone | Status |
|---|---|---|
| M0 | Repo, tooling, compose skeleton | in progress |
| M1 | Data pipeline, single region | in progress |
| M2 | Go API core | in progress |
| M3 | Web app core and design system | not started |
| M4 | Community layer | backend in progress |
| M5 | Navigation | not started |
| M6 | All 18 regions | not started |
| M7 | Polish, PWA, performance, accessibility | not started |
| M8 | Desire paths and moderation | backend in progress |
| M9 | Deploy and docs | not started |

## Open issues

- **Docker/WSL2 unavailable on the build machine**: CPU virtualization is disabled in firmware
  (`HCS_E_HYPERV_NOT_INSTALLED`). Everything runs natively (see DECISIONS.md). To use the Docker
  Compose path, enable Intel VT-x / AMD-V (SVM) in the BIOS/UEFI setup.
