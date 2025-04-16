FROM golang:alpine AS builder

WORKDIR /app

ENV CGO_ENABLED=1

RUN apk add --no-cache \
    # Important: required for go-sqlite3
    gcc \
    # Required for Alpine
    musl-dev

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN apk add --no-cache build-base

RUN CGO_ENABLED=1 go build -ldflags="-w -s" -o /blacked main.go

FROM alpine:latest

WORKDIR /app
COPY --from=builder /blacked /app/blacked

EXPOSE 8082
ENTRYPOINT ["/app/blacked"]
CMD ["serve"]
