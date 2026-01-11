# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download all dependencies. Dependencies will be cached if the go.mod and go.sum files are not changed
RUN go mod download

# Copy the source from the current directory to the Working Directory inside the container
COPY . .

# Build the Go app
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o main .

# Run stage
FROM python:3.12-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates tzdata \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /app

# Copy the Pre-built binary from the previous stage
COPY --from=builder /app/main .

COPY python ./python
RUN pip install --no-cache-dir -r ./python/requirements.txt

ENV MPLCONFIGDIR=/tmp/matplotlib

# Command to run the executable
CMD ["./main"]
