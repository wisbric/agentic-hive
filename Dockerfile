FROM golang:1.26-alpine AS build

RUN apk add --no-cache gcc musl-dev

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG VERSION=dev
ARG COMMIT=unknown
RUN CGO_ENABLED=1 go build -ldflags "-X gitlab.com/adfinisde/agentic-workspace/agentic-hive/internal/server.Version=${VERSION} -X gitlab.com/adfinisde/agentic-workspace/agentic-hive/internal/server.Commit=${COMMIT}" -o /agentic-hive ./cmd/server

FROM alpine:3.21
RUN apk add --no-cache openssh-client ca-certificates
COPY --from=build /agentic-hive /usr/local/bin/agentic-hive

EXPOSE 8080
ENTRYPOINT ["agentic-hive"]
