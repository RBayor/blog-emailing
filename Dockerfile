# Use the official Golang image
FROM golang:1.22.3

# Set the working directory inside the container
WORKDIR /app

# Copy go.mod and go.sum to the working directory
COPY go.mod go.sum ./

# Download Go module dependencies
RUN go mod download

# Copy the entire project to the working directory
COPY . .

# Build the Go application
RUN CGO_ENABLED=1 GOOS=linux go build -o main .

# Create the /data directory and set permissions
RUN mkdir /data && chmod 777 /data

# Expose port 8080 to the outside world
EXPOSE 8080

# Command to run the application
CMD ["./main"]
