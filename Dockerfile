FROM golang:1.26-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# ── Development target (live reload via air) ──────────────────────────────────
FROM builder AS live-dev

RUN apk add curl

RUN curl -sSfL https://raw.githubusercontent.com/air-verse/air/master/install.sh | sh -s -- -b $(go env GOPATH)/bin

EXPOSE 8080
CMD ["air"]

# ── Production build ──────────────────────────────────────────────────────────
FROM builder AS build

RUN CGO_ENABLED=0 GOOS=linux go build -o /app/server ./cmd/server

FROM alpine:latest AS final

RUN addgroup -S app && adduser -S app -G app

WORKDIR /app
COPY --from=build /app/server .

USER app
EXPOSE 8080
CMD ["/app/server"]
