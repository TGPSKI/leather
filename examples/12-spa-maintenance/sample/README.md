# my-spa

A single-page application built with React + TypeScript + Vite.

**Version: 2.3.0**

## Features

- Dashboard with real-time data via WebSocket
- Multi-tenant workspace switching
- Dark mode support
- Keyboard shortcuts (`?` for help)
- Data export to CSV and JSON

## Getting Started

### Requirements

- Node.js 18+
- npm 9+

### Install

```bash
npm install
```

### Development

```bash
npm run dev
```

App runs at `http://localhost:5173` by default.

### Build

```bash
npm run build
```

Output is in `dist/`. Deploy `dist/` to any static host (Netlify, Vercel, S3, etc.).

### Environment Variables

```
VITE_API_URL=https://api.example.com   # backend API base URL
VITE_WS_URL=wss://api.example.com/ws   # WebSocket endpoint
VITE_TENANT_ID=                         # optional: lock to a single tenant
```

Create a `.env.local` file at the project root with these values.

### Tests

```bash
npm test
```

## Deployment

This app deploys automatically via GitHub Actions on push to `main`.
The workflow runs `npm run build` and syncs `dist/` to the configured host.

Check the deploy status badge above or visit https://example.com after each push.

## Contributing

1. Fork the repo
2. Create a feature branch
3. Open a PR against `main`
4. CI must pass before merge
