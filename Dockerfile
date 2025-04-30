FROM golang:alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go build -ldflags="-w -s" -o /blacked main.go

FROM gcr.io/distroless/static-debian12

WORKDIR /app

COPY --from=builder /blacked /app/blacked

COPY --from=builder /app/.env.toml /app/.env.toml

EXPOSE 8082
ENTRYPOINT ["/app/blacked"]
CMD ["serve"]
