# Deployment Runbook — DigitalOcean VPS

## Prerequisites
- DigitalOcean Droplet (Ubuntu 22.04+, 2GB RAM minimum)
- Docker + Docker Compose V2 installed
- Domain or IP for the VPS

## 1. Initial VPS Setup

```bash
# SSH into droplet
ssh root@YOUR_DROPLET_IP

# Install Docker
curl -fsSL https://get.docker.com | sh
apt install -y docker-compose-plugin

# Create app user
useradd -m -s /bin/bash recon
usermod -aG docker recon
su - recon

# Clone repository
git clone https://github.com/kingsleyonoh/Transaction-Reconciliation-Engine.git
cd Transaction-Reconciliation-Engine
```

## 2. Configure Environment

```bash
cp .env.example .env
nano .env
```

**Required variables:**
| Variable | Description | Example |
|---|---|---|
| `DATABASE_URL` | PostgreSQL connection | `postgres://recon:PASSWORD@postgres:5432/reconciliation?sslmode=disable` |
| `REDIS_URL` | Redis connection | `redis://redis:6379/0` |
| `API_KEY` | API authentication key | `your-secure-api-key` |
| `PORT` | HTTP server port | `8080` |
| `POSTGRES_PASSWORD` | DB password for compose | `your-secure-password` |

## 3. Deploy

```bash
# Build and start all services
docker compose up -d --build

# Check status
docker compose ps

# View logs
docker compose logs -f app
```

## 4. Run Migrations

```bash
docker compose exec app /recon migrate
```

## 5. Verify Deployment

```bash
# Health check
curl -sf http://localhost:8080/health | jq .

# Expected:
# { "status": "healthy", "postgres": "ok", "redis": "ok" }
```

## 6. Updates

```bash
cd Transaction-Reconciliation-Engine
git pull origin dev
docker compose up -d --build
```

## 7. Monitoring & Observability

### Health Endpoint

```bash
curl -sf http://YOUR_DOMAIN/health | jq .
# → { "status": "healthy", "postgres": "ok", "redis": "ok" }
```

### Structured Logging (zerolog)

Logs are emitted as JSON to stdout. Docker Compose captures them:

```bash
docker compose logs -f app
# Each line is valid JSON: {"level":"info","time":"...","method":"GET","path":"/health",...}
```

Set `LOG_LEVEL` in `.env` to control verbosity: `debug`, `info` (default), `warn`, `error`.

### Sentry Error Tracking

1. Create a project at [sentry.io](https://sentry.io) (free tier).
2. Copy the DSN and set `SENTRY_DSN` in `.env`.
3. Restart the container — Sentry captures unhandled errors and panics.

### BetterStack Uptime Monitoring

1. Sign up at [betterstack.com](https://betterstack.com) (free tier: 10 monitors, 3-min interval).
2. Create a **HTTP(S) Monitor**:
   - **URL:** `https://YOUR_DOMAIN/health`
   - **Check interval:** 180s (3 min)
   - **Expected status:** 200
   - **Keyword check:** `healthy`
3. Configure alert channels (email, Slack, webhook).
4. Set `BETTERSTACK_URL` in `.env` for reference (not consumed by the app).

### Container Metrics

```bash
docker stats recon-engine
```

