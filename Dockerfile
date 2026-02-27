# Stage 1: Build frontend
FROM node:20-alpine AS frontend
WORKDIR /web
COPY web/package.json web/package-lock.json* ./
RUN npm ci
COPY web/ .
RUN npm run build

# Stage 2: Build Go binary
FROM golang:1.23-alpine AS backend
RUN apk add --no-cache git
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /web/dist internal/server/dist/
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o pgmigrator ./cmd/pgmigrator

# Stage 3: Final image
FROM alpine:3.19
RUN apk add --no-cache ca-certificates postgresql-client
COPY --from=backend /app/pgmigrator /usr/local/bin/pgmigrator
EXPOSE 7654
ENTRYPOINT ["pgmigrator"]
