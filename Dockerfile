# Stage 1: Build frontend
FROM node:22-alpine AS frontend
WORKDIR /web
COPY web/package.json ./
RUN npm install
COPY web/ .
RUN npm run build

# Stage 2: Build Go binary
FROM golang:1.24-alpine AS backend
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /web/dist internal/server/dist/
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o pgmanager ./cmd/pgmanager

# Stage 3: Runtime
FROM alpine:3.21
RUN apk add --no-cache ca-certificates libstdc++ icu-libs zstd-libs lz4-libs libldap krb5-libs
COPY --from=backend /app/pgmanager /usr/local/bin/pgmanager
COPY --from=postgres:18-alpine /usr/local/bin/pg_dump /usr/local/bin/pg_dump
COPY --from=postgres:18-alpine /usr/local/bin/psql /usr/local/bin/psql
COPY --from=postgres:18-alpine /usr/local/lib/libpq.so* /usr/local/lib/
COPY --from=postgres:18-alpine /usr/local/lib/postgresql/ /usr/local/lib/postgresql/
ENV LD_LIBRARY_PATH=/usr/local/lib
EXPOSE 7654
ENTRYPOINT ["pgmanager"]
