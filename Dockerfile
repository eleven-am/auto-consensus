FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /testnode ./cmd/testnode

FROM alpine:3.20

RUN apk --no-cache add ca-certificates

COPY --from=builder /testnode /testnode

EXPOSE 5353/udp 7946 8080

ENTRYPOINT ["/testnode"]
