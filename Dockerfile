FROM golang:1.22-alpine AS builder

WORKDIR /app

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Build binary
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /go/bin/go-mosaic .

# Final minimal runtime image
FROM alpine:3.20

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app
COPY --from=builder /go/bin/go-mosaic /usr/local/bin/go-mosaic

ENTRYPOINT ["go-mosaic"]
CMD ["-h"]
