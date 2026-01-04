FROM golang:alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Enable CGO for SQLite
RUN apk add --no-cache gcc musl-dev
RUN CGO_ENABLED=1 go build -ldflags="-w -s" -o /blacked main.go

FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /app

COPY --from=builder /blacked /app/blacked

COPY --from=builder /app/.env.toml /app/.env.toml

EXPOSE 8082
ENTRYPOINT ["/app/blacked"]
CMD ["serve"]
