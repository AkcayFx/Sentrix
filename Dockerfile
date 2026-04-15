# Stage 1: Build frontend
FROM node:20-alpine AS frontend-builder
WORKDIR /app/frontend
COPY frontend/package.json frontend/package-lock.json* ./
RUN npm ci --ignore-scripts
COPY frontend/ .
RUN npm run build

# Stage 2: Build backend
FROM golang:1.25-alpine AS backend-builder
RUN apk add --no-cache git
WORKDIR /app
COPY backend/go.mod backend/go.sum* ./
RUN go mod download
COPY backend/ .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /sentrix ./cmd/sentrix

# Stage 3: Runtime
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
RUN adduser -D -u 1000 appuser
WORKDIR /app

COPY --from=backend-builder /sentrix .
COPY --from=frontend-builder /app/frontend/dist ./static
COPY backend/migrations ./migrations

USER appuser
EXPOSE 8080
ENTRYPOINT ["./sentrix"]
