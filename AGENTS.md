# AGENTS.md

## Project Overview

caddy-dev-local is a Windows host application that auto-registers `.dev.local` domains for Docker containers. The `devlocal` process attaches to same-host Caddy through its admin API and also manages the hosts file and hosts the dashboard.

## Code Conventions

- Prefer self-documenting code to code comments
