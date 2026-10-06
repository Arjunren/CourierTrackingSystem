# Courier Tracking System

## Overview

A secure Go REST API for creating shipments, dispatching couriers, recording delivery milestones, viewing role-scoped operational data, and sharing privacy-limited public tracking.

## Features

- Customer registration plus admin, dispatcher, courier, and customer roles
- Revocable JWT login and logout
- Shipment creation with generated tracking numbers and expected-delivery dates
- Atomic one-time courier assignment
- Enforced lifecycle: created, picked up, in transit, out for delivery, delivered or cancelled
- Immutable location and status history
- Customer ownership and assigned-courier access checks
- Public tracking that omits sender, recipient, phone, customer, courier, and recorder identities
- Search, status filters, pagination, role-aware dashboards, Docker, CI, and OpenAPI

## Technology Stack

Go 1.25+, Chi, pgx, PostgreSQL 14+, bcrypt, JWT, Docker Compose.

## Requirements

Go 1.25+, PostgreSQL 14+, and optionally Docker with Compose.

## Installation

```bash
git clone https://github.com/Arjunren/courier-tracking-system.git
cd courier-tracking-system
go mod download
cp .env.example .env
```

PowerShell: `Copy-Item .env.example .env`.

## Environment Variables

Configure `APP_ADDR`, `DATABASE_URL`, `JWT_SECRET`, `JWT_EXPIRY`, `ALLOWED_ORIGINS`, and all `SEED_*` values shown in `.env.example`. The sample values are strictly for local development.

## Database Setup

```bash
go run ./cmd/migrate
go run ./cmd/seed
```

## Running the Application

```bash
go run ./cmd/api
```

Docker:

```bash
docker compose up -d db
docker compose --profile tools run --rm migrate
docker compose --profile tools run --rm seed
docker compose up -d api
```

## Running Tests

```bash
go test ./...
go vet ./...
```

CI runs the race detector, a live PostgreSQL concurrent-dispatch test, and all command builds.

## Default Development Accounts

- Admin: `admin@example.com` / `AdminPass123!`
- Dispatcher: `dispatcher@example.com` / `Dispatcher123!`
- Courier: `courier@example.com` / `CourierPass123!`
- Customer: `customer@example.com` / `CustomerPass123!`

## API Endpoints

Public routes are `POST /api/auth/register`, `POST /api/auth/login`, and `GET /api/track/{tracking_number}`. Authenticated routes include `GET /api/dashboard`, admin-only `POST /api/users`, `GET|POST /api/shipments`, `GET /api/shipments/{id}`, dispatcher/admin `POST /api/shipments/{id}/assign`, `POST /api/shipments/{id}/status`, and `GET /api/shipments/{id}/events`. Lists support `page`, `limit`, `search`, and `status`. See `docs/openapi.yaml`.

## Folder Structure

`cmd/` contains API, migration, and seed entry points. `internal/domain` defines models and the status machine; `internal/service` owns validation and access rules; `internal/store` defines persistence; `internal/postgres` implements transactions; `internal/httpapi` provides HTTP transport and middleware; `migrations/` and `docs/` contain schema and API documentation.

## Security Notes

Passwords are bcrypt-hashed and excluded from JSON. JWTs are short-lived and require a live database session. Customer and courier reads are scoped in list queries and rechecked on individual objects. Assignment and status changes lock the shipment row, validate current state, update the shipment, and append a tracking event atomically. Public tracking uses a dedicated DTO and query that omit personal and internal identity fields. JSON bodies are size-limited and reject unknown fields; SQL is parameterized; CORS is allowlisted; login and public tracking are rate-limited; errors are sanitized.

## Known Limitations

- API-only with no mapping, proof-of-delivery uploads, SMS, or email integration.
- Each shipment has one courier and one straight-line lifecycle; transfers and failed-delivery attempts are outside scope.
- Public tracking intentionally exposes origin, destination, event locations, notes, and dates; deployments should review those fields against their privacy policy.
