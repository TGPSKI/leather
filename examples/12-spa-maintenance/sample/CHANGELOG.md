# Changelog

All notable changes to this project will be documented in this file.
Format follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).

## [2.3.1] - 2026-04-28

### Fixed
- Auth token refresh race condition on parallel requests (#312)
- Mobile layout overflow on properties panel at viewport < 375px

## [2.3.0] - 2026-04-15

### Added
- Dark mode toggle with localStorage persistence
- Keyboard shortcuts for navigation (`/` search, `?` help, `j`/`k` navigation)
- Bulk select + export for data tables (CSV and JSON)

### Changed
- Upgraded React Query v4 to TanStack Query v5 API (non-breaking wrapper included)

### Fixed
- WebSocket reconnect loop memory leak after 24h continuous session
- Safari 16 CSS grid rendering glitch on dashboard layout

## [2.2.1] - 2026-03-20

### Fixed
- TLS certificate validation error on staging endpoint
- Memory leak in WebSocket reconnect loop

## [2.2.0] - 2026-03-01

### Added
- Real-time notifications via WebSocket
- Export to CSV/JSON from data tables

### Changed
- Migrated from React Query v4 to TanStack Query v5

## [2.1.0] - 2026-01-10

### Added
- Multi-tenant workspace switching
- Keyboard shortcut help modal (`?`)

## [2.0.0] - 2025-11-15

### Changed
- Complete UI rewrite with new design system
- Migrated from Create React App to Vite
- Dropped IE11 support

## [1.9.3] - 2025-09-01

### Fixed
- Session expiry handling on token revocation
