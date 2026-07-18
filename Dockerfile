# Multi-stage build: compiles the Linux binary in a full Go image, then
# copies just that binary into a minimal runtime image. The app itself has
# no C dependencies (modernc.org/sqlite is pure Go, no cgo), so
# CGO_ENABLED=0 produces a fully static binary that scratch can run.
#
# rsrc_windows_amd64.syso (the Windows exe icon resource) is NOT linked into
# this build: Go only links a *_windows_*.syso file into windows/amd64
# builds, so it's automatically skipped here -- no extra step needed.

FROM golang:1.25-alpine AS build
WORKDIR /app

# alpine's base image doesn't include tzdata -- installed explicitly here so
# the runtime stage below has something to copy zoneinfo data from.
RUN apk add --no-cache tzdata

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o /contacts .

FROM scratch
WORKDIR /app

# scratch has no tzdata -- copy it from the build image so log/backup
# timestamps use local time instead of silently falling back to UTC.
# TZ defaults to Europe/Brussels (this app's locale); override via
# docker-compose.yml or `docker run -e TZ=...` if you're elsewhere.
COPY --from=build /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=build /contacts /app/contacts
ENV TZ=Europe/Brussels

# CONTACTS_LISTEN_HOST is not set here: main.go already defaults to
# 0.0.0.0 on non-Windows, which is what a container needs to be reachable
# from outside. Override it only if you need something more restrictive
# (e.g. behind a reverse proxy on the same docker network).

EXPOSE 8080
VOLUME ["/app/data"]

ENTRYPOINT ["/app/contacts"]
